package executor

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

//go:embed harbor_bridge.py
var nativeHarborBridgeBody string

//go:embed harbor_network.py
var nativeHarborNetwork string

var nativeHarborBridge = nativeHarborTLS + "\n" + nativeHarborNetwork + "\n" + nativeHarborBridgeBody

var harborPythonName = regexp.MustCompile(`(?i)^python(?:[0-9]+(?:\.[0-9]+)*)?(?:\.exe)?$`)

type nativeBridgeRequest struct {
	Operation           string                  `json:"operation"`
	AgentName           string                  `json:"agent_name,omitempty"`
	Dataset             string                  `json:"dataset,omitempty"`
	Task                *domain.BenchmarkTask   `json:"task,omitempty"`
	DownloadDir         string                  `json:"download_dir,omitempty"`
	ConfigPath          string                  `json:"config_path,omitempty"`
	APIKeyName          string                  `json:"api_key_name,omitempty"`
	CACertPath          string                  `json:"ca_cert_path,omitempty"`
	SkipTLSVerification bool                    `json:"skip_tls_verification,omitempty"`
	SourceNetwork       *nativeStageNetwork     `json:"source_network,omitempty"`
	Network             *harborExecutionNetwork `json:"network,omitempty"`
}

type nativeResolution struct {
	Dataset             string                 `json:"dataset"`
	Version             string                 `json:"version"`
	RegistryKind        string                 `json:"registry_kind"`
	RegistryURL         string                 `json:"registry_url"`
	RegistryMetadataURL string                 `json:"registry_metadata_url,omitempty"`
	DatasetDigest       string                 `json:"dataset_digest,omitempty"`
	DatasetRevision     string                 `json:"dataset_revision,omitempty"`
	Tasks               []domain.BenchmarkTask `json:"tasks"`
}

type materializedTask struct {
	Path        string               `json:"path"`
	Name        string               `json:"name"`
	TrialPrefix string               `json:"trial_prefix"`
	Task        domain.BenchmarkTask `json:"task"`
	RegistryURL string               `json:"registry_url"`
}

type nativeBridgeResponse struct {
	OK           bool              `json:"ok"`
	ErrorCode    string            `json:"error_code,omitempty"`
	Resolution   *nativeResolution `json:"resolution,omitempty"`
	Materialized *materializedTask `json:"materialized,omitempty"`
	Trials       int               `json:"trials,omitempty"`
}

func nativeHarborPython(e domain.Environment, harbor string) (string, error) {
	if harbor == "" {
		harbor = e.Tools["harbor"]
	}
	executable, reason := findNative("harbor", harbor)
	if executable == "" {
		return "", errors.New("installed native Harbor is unavailable: " + reason)
	}
	f, err := os.Open(executable)
	if err != nil {
		return "", errors.New("could not read the installed Harbor launcher")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", errors.New("could not read the installed Harbor launcher")
	}
	newline := bytes.IndexByte(data, '\n')
	if newline < 0 || newline > 2048 || !bytes.HasPrefix(data, []byte("#!")) {
		return "", errors.New("unsupported Harbor launcher: its own Python interpreter must be recorded in an absolute shebang")
	}
	python := strings.TrimSpace(string(data[2:newline]))
	if !filepath.IsAbs(python) || !validText(python) || !IsNativeExecutablePath(runtime.GOOS, python) || !harborPythonName.MatchString(filepath.Base(python)) {
		return "", errors.New("unsupported Harbor shebang: no alternate Python interpreter will be used")
	}
	// Do not resolve this symlink: Python uses the venv path to select sys.prefix.
	info, err := os.Stat(python)
	if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		return "", errors.New("Harbor's recorded Python interpreter is unavailable; no alternate interpreter will be used")
	}
	return python, nil
}

func nativeBridgeEnvironment(e domain.Environment, env []string) []string {
	if env != nil {
		return append([]string(nil), env...)
	}
	out := transportEnvironment()
	for key, value := range profileEnv(e, "", "", false) {
		out = append(out, key+"="+value)
	}
	return append(out, "NO_COLOR=1", "PYTHONUNBUFFERED=1")
}

func nativeBridgeTimeout(operation string) time.Duration {
	switch operation {
	case "materialize":
		return 5 * time.Minute
	case "capabilities":
		return 15 * time.Second
	case "validate":
		return 30 * time.Second
	default:
		return time.Minute
	}
}

func nativeBridgeError(code string) error {
	messages := map[string]string{
		"unsupported_api":     "installed Harbor lacks required native dataset, task or JobConfig APIs",
		"unsupported_tls":     "installed Harbor's Python clients cannot honor the explicit scoped TLS policy",
		"unsupported_network": "installed Harbor cannot honor the explicit source, build, agent or strict verifier network policy",
		"invalid_request":     "invalid bounded native Harbor bridge request",
		"invalid_source":      "native Harbor returned unsafe, ambiguous or unresolved source metadata",
		"resolution_failed":   "native Harbor dataset resolution failed; verify network, access and task availability",
		"download_failed":     "native Harbor exact task download failed; verify network and recorded source availability",
		"task_missing":        "native Harbor did not materialize a valid original task and verifier",
		"task_mismatch":       "native Harbor downloaded a different source than the saved task reference",
		"cached_task":         "native Harbor reused a task cache instead of downloading the recorded source",
		"invalid_directory":   "native Harbor task download requires an empty private per-attempt directory",
		"invalid_job":         "native Harbor rejected the one-task JobConfig or its agent options",
		"contract_ignored":    "native Harbor schema or trial expansion did not honor the required execution contract",
		"response_limit":      "native Harbor source metadata exceeded its bounded response limit",
	}
	if message, ok := messages[code]; ok {
		return errors.New(message)
	}
	return errors.New("native Harbor bridge failed without a supported sanitized error")
}

func runNativeHarborBridge(ctx context.Context, e domain.Environment, harbor string, request nativeBridgeRequest, env []string) (nativeBridgeResponse, error) {
	var response nativeBridgeResponse
	python, err := nativeHarborPython(e, harbor)
	if err != nil {
		return response, err
	}
	if env == nil {
		selected, source, operationEnv, closeNetwork, err := prepareSourceOperation(ctx, e)
		if err != nil {
			return response, err
		}
		defer closeNetwork()
		e, env, request.SourceNetwork = selected, operationEnv, source
	} else if request.SourceNetwork == nil {
		profile, _, err := sourceNetwork(e)
		if err != nil {
			return response, err
		}
		request.SourceNetwork = &nativeStageNetwork{Profile: profile, Env: explicitNetworkEnvironment(env), Mounts: []HarborMount{}}
	}
	if request.SourceNetwork.Env["ROUTER_FORGE_NETWORK_STAGE"] != "source" {
		return response, errors.New("native source operation requires its prepared isolated source configuration")
	}
	request.SkipTLSVerification = request.SourceNetwork.Profile.TLSMode == domain.NetworkSkipTLS
	request.CACertPath = request.SourceNetwork.Env["SSL_CERT_FILE"]
	argument, err := json.Marshal(request)
	if err != nil || len(argument) > maxControlBody {
		return response, errors.New("native Harbor request exceeded its byte limit")
	}
	probe, cancel := context.WithTimeout(ctx, nativeBridgeTimeout(request.Operation))
	defer cancel()
	script, err := ownedNativeBridgeScript(request.SourceNetwork.Env["CURL_HOME"])
	if err != nil {
		return response, err
	}
	command := Command{Path: python, Args: []string{"-I", "-B", script, string(argument)}}
	data, _, runErr := runCommand(probe, command, nativeBridgeEnvironment(e, env), "", nil, maxControlBody)
	if probe.Err() != nil {
		return response, fmt.Errorf("native Harbor bridge was cancelled or timed out: %w", probe.Err())
	}
	if len(data) == 0 || json.Unmarshal(data, &response) != nil {
		return response, errors.New("native Harbor returned an incomplete, oversized or invalid bridge response")
	}
	if !response.OK {
		return response, nativeBridgeError(response.ErrorCode)
	}
	if runErr != nil || response.ErrorCode != "" {
		return response, errors.New("native Harbor bridge did not complete successfully")
	}
	return response, nil
}

func ownedNativeBridgeScript(directory string) (string, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return "", errors.New("native bridge requires an owned absolute source configuration directory")
	}
	filename := filepath.Join(directory, "native-bridge.py")
	info, err := os.Lstat(filename)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(nativeHarborBridge)) {
			return "", errors.New("owned native bridge script is unsafe or changed")
		}
		data, err := os.ReadFile(filename)
		if err != nil || string(data) != nativeHarborBridge {
			return "", errors.New("owned native bridge script is unsafe or changed")
		}
		return filename, nil
	}
	if !os.IsNotExist(err) || writePrivate(filename, []byte(nativeHarborBridge)) != nil {
		return "", errors.New("could not persist owned native bridge script")
	}
	return filename, nil
}

func normalizeNativeResolution(source *nativeResolution) (domain.ResolvedBenchmark, error) {
	var resolved domain.ResolvedBenchmark
	suite := domain.SWEBenchVerified()
	if source == nil || source.Dataset != suite.Dataset || len(source.Tasks) == 0 || len(source.Tasks) > 10000 {
		return resolved, errors.New("native Harbor resolution did not return the required dataset and actual tasks")
	}
	tasks := append([]domain.BenchmarkTask(nil), source.Tasks...)
	for i := range tasks {
		if tasks[i].Kind == "git" {
			tasks[i].GitCommit = strings.ToLower(tasks[i].GitCommit)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	suite.Version, suite.RegistryURL, suite.TaskCount = source.Version, source.RegistryURL, len(tasks)
	suite.TaskRepository, suite.TaskCommit, suite.SuggestedTask = "", "", ""
	if tasks[0].Kind == "git" {
		suite.TaskRepository, suite.TaskCommit = tasks[0].GitURL, tasks[0].GitCommit
		for _, task := range tasks {
			if task.Kind != "git" || task.GitURL != suite.TaskRepository || task.GitCommit != suite.TaskCommit {
				suite.TaskRepository, suite.TaskCommit = "", ""
				break
			}
		}
	}
	resolved = domain.ResolvedBenchmark{Suite: suite, RegistryKind: source.RegistryKind, RegistryMetadataURL: source.RegistryMetadataURL, DatasetDigest: source.DatasetDigest, DatasetRevision: source.DatasetRevision, Tasks: tasks, ResolvedAt: domain.Now()}
	if err := resolved.Validate(); err != nil {
		return domain.ResolvedBenchmark{}, fmt.Errorf("native Harbor resolved source is invalid: %w", err)
	}
	hash, err := resolved.Hash()
	if err != nil {
		return domain.ResolvedBenchmark{}, errors.New("could not hash the native Harbor resolution")
	}
	resolved.ResolutionHash = hash
	data, err := json.Marshal(resolved)
	if err != nil || len(data) > maxControlBody-4096 {
		return domain.ResolvedBenchmark{}, errors.New("resolved Harbor dataset exceeded the private control transport limit")
	}
	return resolved, nil
}

func privateHarborDownloadDir(dir string) error {
	if !filepath.IsAbs(dir) || len(dir) > 2048 || !validText(dir) || filepath.Clean(dir) != dir || dir == filepath.VolumeName(dir)+string(os.PathSeparator) {
		return nativeBridgeError("invalid_directory")
	}
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return nativeBridgeError("invalid_directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nativeBridgeError("invalid_directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		return nativeBridgeError("invalid_directory")
	}
	return nil
}

func materializeHarborTask(ctx context.Context, e domain.Environment, harbor string, task domain.BenchmarkTask, downloadDir string, env []string) (materializedTask, error) {
	var materialized materializedTask
	if err := task.Validate(); err != nil {
		return materialized, err
	}
	if err := privateHarborDownloadDir(downloadDir); err != nil {
		return materialized, err
	}
	response, err := runNativeHarborBridge(ctx, e, harbor, nativeBridgeRequest{Operation: "materialize", Task: &task, DownloadDir: downloadDir}, env)
	if err != nil {
		return materialized, err
	}
	if response.Materialized == nil {
		return materialized, nativeBridgeError("task_missing")
	}
	materialized = *response.Materialized
	if materialized.Task != task || materialized.Task.Validate() != nil || !domain.ValidTaskName(materialized.Name) || !domain.ValidTaskName(materialized.TrialPrefix) || strings.Contains(materialized.TrialPrefix, "/") || !domain.ValidSourceURL(materialized.RegistryURL) || !filepath.IsAbs(materialized.Path) || len(materialized.Path) > 2048 || !validText(materialized.Path) {
		return materializedTask{}, nativeBridgeError("task_mismatch")
	}
	root, rootErr := filepath.EvalSymlinks(downloadDir)
	actual, pathErr := filepath.EvalSymlinks(materialized.Path)
	relative, relErr := filepath.Rel(root, actual)
	info, statErr := os.Stat(actual)
	if rootErr != nil || pathErr != nil || relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || statErr != nil || !info.IsDir() {
		return materializedTask{}, nativeBridgeError("task_missing")
	}
	materialized.Path = actual
	return materialized, nil
}

func validateNativeHarborJob(ctx context.Context, e domain.Environment, harbor string, configPath string, env []string, networks ...*harborExecutionNetwork) error {
	if !filepath.IsAbs(configPath) || len(configPath) > 2048 || !validText(configPath) {
		return nativeBridgeError("invalid_job")
	}
	request := nativeBridgeRequest{Operation: "validate", ConfigPath: configPath, APIKeyName: attemptKeyName, CACertPath: e.CACertPath}
	if len(networks) != 0 {
		request.Network = networks[0]
		request.SourceNetwork = networks[0].Source
	}
	response, err := runNativeHarborBridge(ctx, e, harbor, request, env)
	if err != nil {
		return err
	}
	if response.Trials != 1 {
		return nativeBridgeError("contract_ignored")
	}
	return nil
}

func checkNativeHarborCapabilities(ctx context.Context, e domain.Environment, harbor string, environments ...[]string) error {
	var env []string
	if len(environments) != 0 {
		env = environments[0]
	}
	_, err := runNativeHarborBridge(ctx, e, harbor, nativeBridgeRequest{Operation: "capabilities"}, env)
	return err
}

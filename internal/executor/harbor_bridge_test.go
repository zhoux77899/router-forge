package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func nativeBridgeFixtureResolution() *nativeResolution {
	digest := "sha256:" + strings.Repeat("a", 64)
	return &nativeResolution{Dataset: domain.SWEBenchVerified().Dataset, Version: digest, RegistryKind: "package", RegistryURL: "https://installed.fixture.test/hub", RegistryMetadataURL: "https://physical.fixture.test/native-db", DatasetDigest: digest, DatasetRevision: "native-version-record", Tasks: []domain.BenchmarkTask{
		{Name: "fixture/task-z", Kind: "package", PackageName: "fixture/task-z", Ref: "sha256:" + strings.Repeat("c", 64)},
		{Name: "fixture/task-a", Kind: "package", PackageName: "fixture/task-a", Ref: "sha256:" + strings.Repeat("b", 64)},
	}}
}

// The copied test executable is an explicit fixture interpreter, never a fallback.
func init() {
	if len(os.Args) != 5 || os.Args[1] != "-I" || os.Args[2] != "-c" || os.Args[3] != nativeHarborBridge {
		return
	}
	input, inputErr := io.ReadAll(os.Stdin)
	if inputErr != nil || len(input) != 0 {
		os.Exit(4)
	}
	if os.Getenv("RF_BRIDGE_WAIT") == "1" {
		time.Sleep(time.Minute)
	}
	if os.Getenv("RF_BRIDGE_MALFORMED") == "1" {
		fmt.Print("not JSON")
		os.Exit(0)
	}
	if os.Getenv("RF_BRIDGE_OVERSIZED") == "1" {
		fmt.Print(strings.Repeat("x", maxControlBody+1))
		os.Exit(0)
	}
	var request nativeBridgeRequest
	if json.Unmarshal([]byte(os.Args[4]), &request) != nil {
		os.Exit(3)
	}
	response := nativeBridgeResponse{OK: true}
	switch request.Operation {
	case "resolve":
		if request.Dataset != domain.SWEBenchVerified().Dataset {
			os.Exit(3)
		}
		response.Resolution = nativeBridgeFixtureResolution()
	case "materialize":
		if request.Task == nil {
			os.Exit(3)
		}
		path := filepath.Join(request.DownloadDir, "native-layout", "original-task")
		if os.MkdirAll(path, 0700) != nil {
			os.Exit(3)
		}
		response.Materialized = &materializedTask{Path: path, Name: "actual__native-task-name", TrialPrefix: "task-z", Task: *request.Task, RegistryURL: "https://installed.fixture.test/hub"}
	case "validate":
		response.Trials = 1
		if os.Getenv("RF_BRIDGE_EXTRA_TRIAL") == "1" {
			response.Trials = 2
		}
	case "capabilities":
	default:
		os.Exit(3)
	}
	if os.Getenv("RF_BRIDGE_ERROR") == "1" {
		response.OK, response.ErrorCode = false, "untrusted-secret-in-native-output"
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
	if !response.OK {
		os.Exit(1)
	}
	os.Exit(0)
}

func bridgeFixtureLauncher(t *testing.T) (domain.Environment, string) {
	t.Helper()
	root := projectTestDir(t)
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonName += ".exe"
	}
	python := filepath.Join(root, pythonName)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(python, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy explicit fixture interpreter: %v, %v", copyErr, closeErr)
	}
	harbor := filepath.Join(root, "harbor")
	if err := os.WriteFile(harbor, []byte("#!"+python+"\nfixture Harbor launcher\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return domain.Environment{Tools: map[string]string{"harbor": harbor}}, root
}

func TestNativeHarborBridgeResolveAndTasksUseInstalledInterpreter(t *testing.T) {
	environment, _ := bridgeFixtureLauncher(t)
	worker := &nativeWorker{clientFactory: func(domain.Environment) (*http.Client, error) {
		t.Fatal("native resolution used manual registry fetch")
		return nil, nil
	}}
	suite := domain.SWEBenchVerified()
	suite.Version = "must-not-be-supplied-to-native-default"
	resolved, err := worker.Resolve(context.Background(), environment, suite)
	if err != nil || resolved.Validate() != nil || len(resolved.Tasks) != 2 || resolved.Tasks[0].Name != "fixture/task-a" || resolved.Suite.TaskCount != 2 || !domain.ValidHash(resolved.ResolutionHash) {
		t.Fatalf("native resolution = %#v, %v", resolved, err)
	}
	if resolved.Suite.TaskCommit != "" || resolved.Suite.TaskRepository != "" || resolved.DatasetDigest == "" || resolved.DatasetRevision != "native-version-record" {
		t.Fatal("package metadata was presented as an invented Git source")
	}
	tasks, err := worker.Tasks(context.Background(), environment, domain.SWEBenchVerified())
	if err != nil || len(tasks) != 2 || tasks[0] != resolved.Tasks[0] {
		t.Fatalf("native Tasks compatibility wrapper = %#v, %v", tasks, err)
	}
	if err := checkNativeHarborCapabilities(context.Background(), environment, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNativeHarborBridgeNormalizationHashCountAndTransport(t *testing.T) {
	source := nativeBridgeFixtureResolution()
	for i := 2; i < 500; i++ {
		name := fmt.Sprintf("fixture/task-%03d", i)
		source.Tasks = append(source.Tasks, domain.BenchmarkTask{Name: name, Kind: "package", PackageName: name, Ref: "sha256:" + strings.Repeat("d", 64)})
	}
	resolved, err := normalizeNativeResolution(source)
	if err != nil || resolved.Suite.TaskCount != 500 {
		t.Fatalf("native normalization: %v", err)
	}
	encoded, _ := json.Marshal(resolved)
	if len(encoded) >= maxControlBody {
		t.Fatal("500 normalized tasks exceeded the private control body")
	}
	for i, j := 0, len(source.Tasks)-1; i < j; i, j = i+1, j-1 {
		source.Tasks[i], source.Tasks[j] = source.Tasks[j], source.Tasks[i]
	}
	second, err := normalizeNativeResolution(source)
	if err != nil || second.ResolutionHash != resolved.ResolutionHash {
		t.Fatal("resolution hash depends on native enumeration order or observation time")
	}
}

func TestNativeHarborBridgeGitSourcesAreNotAssumedUniform(t *testing.T) {
	source := &nativeResolution{Dataset: domain.SWEBenchVerified().Dataset, Version: "native-default-selected", RegistryKind: "harbor", RegistryURL: "https://installed.fixture.test/registry", Tasks: []domain.BenchmarkTask{
		{Name: "task-z", Kind: "git", Path: "unusual/task-z", GitURL: "https://code.fixture.test/one.git", GitCommit: strings.Repeat("A", 40)},
		{Name: "task-a", Kind: "git", Path: "different/layout/task-a", GitURL: "https://code.fixture.test/two.git", GitCommit: strings.Repeat("b", 64)},
	}}
	resolved, err := normalizeNativeResolution(source)
	if err != nil || resolved.Suite.TaskCount != 2 || resolved.Suite.TaskCommit != "" || resolved.Suite.TaskRepository != "" || resolved.Tasks[1].GitCommit != strings.Repeat("a", 40) {
		t.Fatalf("heterogeneous native Git resolution: %#v, %v", resolved, err)
	}
	source.Tasks[1].GitURL, source.Tasks[1].GitCommit = source.Tasks[0].GitURL, source.Tasks[0].GitCommit
	resolved, err = normalizeNativeResolution(source)
	if err != nil || resolved.Suite.TaskRepository == "" || resolved.Suite.TaskCommit != strings.Repeat("a", 40) {
		t.Fatal("uniform native Git aggregate was not derived from actual task sources")
	}
}

func TestNativeHarborBridgeRejectsUnsafeOrUnresolvedMetadata(t *testing.T) {
	cases := []func(*nativeResolution){
		func(r *nativeResolution) { r.Dataset = "swebench-verified" },
		func(r *nativeResolution) { r.RegistryURL += "?token=secret" },
		func(r *nativeResolution) { r.RegistryMetadataURL = "https://token@host.fixture.test/data" },
		func(r *nativeResolution) { r.DatasetDigest = "" },
		func(r *nativeResolution) { r.Tasks[0].Ref = "latest" },
		func(r *nativeResolution) { r.Tasks[1] = r.Tasks[0] },
		func(r *nativeResolution) { r.Tasks = nil },
		func(r *nativeResolution) { r.Tasks[0].Kind = "local" },
		func(r *nativeResolution) {
			r.Tasks = []domain.BenchmarkTask{{Name: "task", Kind: "git", Path: "task", GitURL: "https://code.fixture.test/repo.git", GitCommit: "main"}}
		},
	}
	for index, mutate := range cases {
		source := nativeBridgeFixtureResolution()
		mutate(source)
		if _, err := normalizeNativeResolution(source); err == nil {
			t.Fatalf("accepted invalid native source case %d", index)
		}
	}
}

func TestNativeHarborPythonUsesAuthoritativeShebangAndPreservesVenvSymlink(t *testing.T) {
	root := projectTestDir(t)
	python := filepath.Join(root, "python")
	if err := os.WriteFile(python, []byte("fixture interpreter"), 0700); err != nil {
		t.Fatal(err)
	}
	harbor := filepath.Join(root, "harbor")
	if err := os.WriteFile(harbor, []byte("#!"+python+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	environment := domain.Environment{Tools: map[string]string{"harbor": harbor, "python": filepath.Join(root, "alternate-python")}}
	found, err := nativeHarborPython(environment, "")
	if err != nil || found != python {
		t.Fatalf("authoritative Python = %q, %v", found, err)
	}
	venv := filepath.Join(root, "venv")
	if err := os.Mkdir(venv, 0700); err != nil {
		t.Fatal(err)
	}
	venvPython := filepath.Join(venv, "python")
	if err := os.Symlink(python, venvPython); err != nil {
		t.Skip("host does not permit creating fixture symlinks")
	}
	if err := os.WriteFile(harbor, []byte("#!"+venvPython+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	found, err = nativeHarborPython(environment, "")
	if err != nil || found != venvPython {
		t.Fatalf("Python venv symlink was resolved to another prefix: %q, %v", found, err)
	}
}

func TestNativeHarborPythonNeverUsesAlternateInterpreter(t *testing.T) {
	root := projectTestDir(t)
	harbor := filepath.Join(root, "harbor")
	for _, launcher := range []string{"binary launcher", "#!/usr/bin/env python\n", "#!python\n", "#!/bin/sh\n", "#!" + filepath.Join(root, "python") + "\n"} {
		if err := os.WriteFile(harbor, []byte(launcher), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := nativeHarborPython(domain.Environment{Tools: map[string]string{"harbor": harbor}}, ""); err == nil {
			t.Fatal("unsupported or missing Harbor-owned Python used an alternate interpreter")
		}
	}
}

func TestNativeHarborBridgeMaterializationAndValidationAreBounded(t *testing.T) {
	environment, root := bridgeFixtureLauncher(t)
	task := nativeBridgeFixtureResolution().Tasks[0]
	directory := filepath.Join(root, "download")
	materialized, err := materializeHarborTask(context.Background(), environment, "", task, directory, transportEnvironment())
	if err != nil || materialized.Task != task || materialized.Name != "actual__native-task-name" || materialized.TrialPrefix != "task-z" || materialized.Name == materialized.TrialPrefix {
		t.Fatalf("exact materialization = %#v, %v", materialized, err)
	}
	if _, err := materializeHarborTask(context.Background(), environment, "", task, directory, transportEnvironment()); err == nil {
		t.Fatal("materialization reused a previous download/cache directory")
	}
	config := filepath.Join(root, "job.json")
	if err := os.WriteFile(config, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeHarborJob(context.Background(), environment, "", config, transportEnvironment()); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeHarborJob(context.Background(), environment, "", config, append(transportEnvironment(), "RF_BRIDGE_EXTRA_TRIAL=1")); err == nil {
		t.Fatal("native expansion with two actual trials was accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = runNativeHarborBridge(ctx, environment, "", nativeBridgeRequest{Operation: "capabilities"}, append(transportEnvironment(), "RF_BRIDGE_WAIT=1"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bridge process timeout did not propagate: %v", err)
	}
	for _, mode := range []string{"RF_BRIDGE_MALFORMED=1", "RF_BRIDGE_ERROR=1", "RF_BRIDGE_OVERSIZED=1"} {
		_, err := runNativeHarborBridge(context.Background(), environment, "", nativeBridgeRequest{Operation: "capabilities"}, append(transportEnvironment(), mode))
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("native error output was accepted or leaked: %v", err)
		}
	}
}

func TestNativeHarborBridgeOfflineNativeAPIs(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("host Python is only used to run offline stdlib bridge tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, _, err := runCommand(ctx, Command{Path: python, Args: []string{"-I", "-B", "harbor_bridge_test.py"}}, transportEnvironment(), "", nil, 32768)
	if err != nil {
		t.Fatalf("offline native API tests failed: %v\n%s", err, output)
	}
}

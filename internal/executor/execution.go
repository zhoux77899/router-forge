package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func nativeProcessEnv(e domain.Environment, attemptDir, token string) []string {
	if e.Networks != nil {
		e = clearLegacyNetwork(e)
	}
	base := transportEnvironment()
	out := make([]string, 0, len(base)+24)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(key) {
		case "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SSH_AUTH_SOCK", "TMPDIR", "TEMP", "TMP":
			continue
		}
		out = append(out, kv)
	}
	directories := []string{}
	for _, name := range []string{"docker", "harbor", "uv"} {
		if tool := e.Tools[name]; tool != "" {
			directories = append(directories, filepath.Dir(tool))
		}
	}
	for i, kv := range out {
		if strings.HasPrefix(kv, "PATH=") {
			out[i] = "PATH=" + strings.Join(append(directories, strings.TrimPrefix(kv, "PATH=")), string(os.PathListSeparator))
			break
		}
	}
	home := filepath.Join(attemptDir, "home")
	scratch := filepath.Join(attemptDir, "scratch")
	out = append(out, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"), "XDG_DATA_HOME="+filepath.Join(home, "data"), "TMPDIR="+scratch, "TEMP="+scratch, "TMP="+scratch, "PYTHONUNBUFFERED=1", "NO_COLOR=1", "TERM=dumb")
	if token != "" {
		out = append(out, attemptKeyName+"="+token)
	}
	for key, value := range profileEnv(e, "router-forge-gateway", e.DockerHostGateway, false) {
		out = append(out, key+"="+value)
	}
	if e.SkipTLSVerification {
		directory := tlsProfileDir(attemptDir)
		out = append(out, "CURL_HOME="+directory, "WGETRC="+filepath.Join(directory, "wgetrc"), "PYTHONPATH="+filepath.Join(directory, "python"))
	}
	return out
}
func toolsFromInspection(inspection domain.Inspection) map[string]string {
	tools := map[string]string{}
	for _, tool := range inspection.Tools {
		if tool.Available {
			tools[tool.Name] = tool.Path
		}
	}
	return tools
}
func canonicalSchema(schema json.RawMessage) string {
	var data any
	if json.Unmarshal(schema, &data) != nil {
		return ""
	}
	b, _ := json.Marshal(data)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func reserveAttempt(workDir string, spec domain.ExecutionSpec) (string, error) {
	if workDir == filepath.VolumeName(workDir)+string(os.PathSeparator) {
		return "", errors.New("work directory cannot be a filesystem root")
	}
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return "", errors.New("user work directory is not writable")
	}
	runDir := filepath.Join(workDir, spec.RunID)
	if err := privateDir(runDir); err != nil {
		return "", errors.New("could not create private run directory")
	}
	attemptDir := filepath.Join(runDir, spec.AttemptID)
	if err := os.Mkdir(attemptDir, 0700); err != nil {
		return "", errors.New("attempt directory already exists or is not writable; existing attempts are never resumed, overwritten or deleted")
	}
	if err := securePath(attemptDir, true); err != nil {
		return "", err
	}
	for _, name := range []string{"home", "scratch", "jobs"} {
		if err := privateDir(filepath.Join(attemptDir, name)); err != nil {
			return "", err
		}
	}
	if spec.Snapshot.Network == nil && spec.Snapshot.Environment.SkipTLSVerification {
		if err := writeTLSProfile(attemptDir); err != nil {
			return "", err
		}
	}
	return attemptDir, nil
}
func lifecycle(emit func(domain.WorkerEvent), kind, message string) {
	if emit != nil {
		emit(domain.WorkerEvent{Kind: kind, Message: message, Timestamp: domain.Now()})
	}
}
func infrastructureResult(message, path string) domain.ExecutionResult {
	return domain.ExecutionResult{State: "infrastructure_error", Error: message, ExitCode: -1, ResultPath: path, ImageDigests: []string{}}
}
func (w *nativeWorker) Execute(ctx context.Context, e domain.Environment, spec domain.ExecutionSpec, emit func(domain.WorkerEvent), observe func(domain.ExecutionObservation) error) (out domain.ExecutionResult, executionErr error) {
	if err := ValidateExecutionSpec(spec); err != nil {
		return infrastructureResult(err.Error(), ""), err
	}
	if observe == nil || spec.Snapshot.Runtime != nil {
		err := errors.New("a new execution requires durable observation acknowledgments and cannot resume an observed attempt")
		return infrastructureResult(err.Error(), ""), err
	}
	if environmentFingerprint(publicEnvironment(e)) != environmentFingerprint(publicEnvironment(spec.Snapshot.Environment)) {
		err := errors.New("execution environment differs from the approved run configuration")
		return infrastructureResult(err.Error(), ""), err
	}
	e.NetworkRuntime = spec.Networks
	redact := func(value string) string { return redactCommandText(value, workerCommand{Spec: &spec}) }
	defer func() {
		out = privateExecutionResult(out, redact)
		if executionErr != nil && redact(executionErr.Error()) != executionErr.Error() {
			executionErr = errors.New(redact(executionErr.Error()))
		}
	}()
	lifecycle(emit, "inspecting", "Checking installed native tools and current agent capabilities")
	inspection, err := w.Inspect(ctx, e)
	if err != nil {
		return infrastructureResult("execution environment inspection failed", ""), err
	}
	if err := recordPrivateObservation(&spec, observe, domain.ExecutionObservation{Kind: "tools", Timestamp: domain.Now(), Tools: inspection.Tools, WorkDir: inspection.WorkDir, DockerGateway: inspection.DockerGateway}); err != nil {
		return infrastructureResult(err.Error(), ""), err
	}
	if !inspection.Ready {
		err := errors.New("execution environment is not ready; inspect its explicit capability issues")
		return infrastructureResult(err.Error(), ""), err
	}
	agent, err := requireAgent(inspection, spec)
	if err != nil {
		return infrastructureResult(err.Error(), ""), err
	}
	spec.Snapshot.Agent = agent
	tools := toolsFromInspection(inspection)
	e.Tools = tools
	e.DockerHostGateway = inspection.DockerGateway
	attemptDir, err := reserveAttempt(inspection.WorkDir, spec)
	if err != nil {
		return infrastructureResult(err.Error(), ""), err
	}
	jobsDir := filepath.Join(attemptDir, "jobs")
	jobDir := filepath.Join(jobsDir, spec.JobName)
	resultPath := filepath.Join(jobDir, "result.json")
	task := spec.Snapshot.ResolvedTasks[0]
	provenancePath := filepath.Join(attemptDir, "provenance.json")
	initialProvenance := map[string]any{"snapshot_format_version": domain.SnapshotFormatVersion, "config_hash": spec.Snapshot.ConfigHash, "task": task, "runtime": spec.Snapshot.Runtime, "tools": inspection.Tools}
	setNetworkProvenance(initialProvenance, spec.Snapshot)
	initialBytes, _ := json.MarshalIndent(initialProvenance, "", "  ")
	if err := writePrivate(provenancePath, initialBytes); err != nil {
		return infrastructureResult("could not persist initial private network provenance", resultPath), err
	}
	lateProvenance := false
	defer func() {
		if !lateProvenance {
			out = privateExecutionResult(out, redact)
			if err := finalizeProvenance(provenancePath, initialProvenance, spec.Snapshot, out); err != nil {
				if out.Error != "" {
					out.Error += "; "
				}
				out.Error += err.Error()
				if executionErr == nil {
					executionErr = err
				}
			}
		}
	}()
	owner := domain.NewID("owner")
	networkTracker := newDockerTracker(tools["docker"], nativeProcessEnv(clearLegacyNetwork(e), attemptDir, ""), owner, attemptDir, jobDir, spec)
	network, err := prepareExecutionNetwork(ctx, spec, attemptDir, inspection.DockerGateway, networkTracker)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	defer func() { network.close(); _ = os.RemoveAll(filepath.Join(attemptDir, "network")) }()
	if _, err := runNativeHarborBridge(ctx, e, tools["harbor"], nativeBridgeRequest{Operation: "capabilities", AgentName: agent.Name, SourceNetwork: network.Source}, stageProcessEnvironment(e, attemptDir, "", network.Source)); err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	lifecycle(emit, "resolving_task", "Downloading the exact task source recorded in the draft using installed Harbor")
	sourceCtx, stopSource := context.WithTimeout(ctx, time.Duration(spec.Snapshot.Spec.SetupTimeoutSec)*time.Second)
	materialized, err := materializeHarborTask(sourceCtx, e, tools["harbor"], task, filepath.Join(attemptDir, "task-cache"), stageProcessEnvironment(e, attemptDir, "", network.Source))
	stopSource()
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	if materialized.Task != task || !simpleName.MatchString(materialized.TrialPrefix) {
		err := errors.New("native Harbor materialization differs from the approved source or lacks a valid native trial prefix")
		return infrastructureResult(err.Error(), resultPath), err
	}
	materialized.Path, err = privateTaskRoot(attemptDir, materialized.Path)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	if err := recordPrivateObservation(&spec, observe, domain.ExecutionObservation{Kind: "task", Timestamp: domain.Now(), Task: &task, RegistryURL: materialized.RegistryURL, TaskPath: materialized.Path, TaskName: materialized.Name, TrialPrefix: materialized.TrialPrefix}); err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	metadataBytes, err := readTaskSourceFile(materialized.Path, "task.toml", 128<<10)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	metadata, err := parseTaskMetadata(metadataBytes)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	if metadata.VerifierMode == "separate" && !repoDigestPattern.MatchString(metadata.VerifierImage) {
		err := errors.New("the original separate verifier must declare an immutable image because its native factory may not preserve the agent image overlay")
		return infrastructureResult(err.Error(), resultPath), err
	}
	duration := time.Duration(spec.Snapshot.Spec.TimeoutSec+spec.Snapshot.Spec.SetupTimeoutSec+metadata.BuildTimeoutSec+metadata.VerifierTimeoutSec+180) * time.Second
	jobCtx, stopJob := context.WithTimeout(ctx, duration)
	defer stopJob()
	processEnv := stageProcessEnvironment(e, attemptDir, spec.GatewayToken, network.Source)
	tracker := networkTracker
	tracker.spec = spec
	tracker.taskName, tracker.trialPrefix = materialized.Name, materialized.TrialPrefix
	tracker.observer = func(value domain.ExecutionObservation) error {
		return recordPrivateObservation(&spec, observe, value)
	}
	provenance := initialProvenance
	lateProvenance = true
	defer func() {
		network.close()
		version, _, _ := tracker.snapshot()
		if version != "" {
			out.AgentVersion = version
		}
		tracker.addDigests(out.ImageDigests...)
		_, out.ImageDigests, _ = tracker.snapshot()
		if err := tracker.cleanup(); err != nil {
			lifecycle(emit, "cleanup_incomplete", "Owned Docker cleanup was incomplete; no unrelated resources were modified")
			if out.Error != "" {
				out.Error += "; "
			}
			out.Error += err.Error()
			if executionErr == nil {
				executionErr = err
				if out.State == "completed" {
					out.State = "infrastructure_error"
				}
			}
		}
		if provenance != nil {
			out = privateExecutionResult(out, redact)
			if err := finalizeProvenance(provenancePath, provenance, spec.Snapshot, out); err != nil {
				lifecycle(emit, "provenance_incomplete", "Final execution evidence could not be persisted; acknowledged database observations remain available")
				if out.Error != "" {
					out.Error += "; "
				}
				out.Error += err.Error()
				if executionErr == nil {
					executionErr = err
				}
				if out.State == "completed" {
					out.State = "infrastructure_error"
				}
			}
		}
	}()
	lifecycle(emit, "preparing", "Resolving original task image identities; no inference has started")
	image := ""
	probeImage := ""
	pinnedDockerfile := ""
	baseDigests := []string{}
	prepareCtx, stopPrepare := context.WithTimeout(jobCtx, time.Duration(metadata.BuildTimeoutSec)*time.Second)
	defer stopPrepare()
	if metadata.Image != "" {
		frozen, err := dockerImageIdentity(prepareCtx, tools["docker"], metadata.Image, tracker.env)
		if err != nil {
			return infrastructureResult(err.Error(), resultPath), err
		}
		image = frozen.Digest
		probeImage = image
		tracker.expectedImageID = frozen.ID
		tracker.addDigests(frozen.Digest, frozen.ID)
	} else {
		original, err := readTaskSourceFile(materialized.Path, "environment/Dockerfile", 1<<20)
		if err != nil {
			return infrastructureResult(err.Error(), resultPath), err
		}
		pinned, digests, err := FreezeDockerfile(original, func(reference string) (string, error) {
			frozen, err := dockerImageIdentity(prepareCtx, tools["docker"], reference, tracker.env)
			if err != nil {
				return "", err
			}
			return frozen.Digest, nil
		})
		if err != nil {
			return infrastructureResult(err.Error(), resultPath), err
		}
		if len(digests) == 0 {
			err := errors.New("SWE task native build has no Docker-reachable Linux probe base image")
			return infrastructureResult(err.Error(), resultPath), err
		}
		pinnedDockerfile = filepath.Join(attemptDir, "pinned.Dockerfile")
		if err = writePrivate(pinnedDockerfile, pinned); err != nil {
			return infrastructureResult("could not persist pinned native task Dockerfile", resultPath), err
		}
		baseDigests = digests
		tracker.addDigests(digests...)
		image = taskImageTag(spec)
		probeImage = digests[0]
	}
	relay, err := startRelay(jobCtx, w.gateway, inspection.DockerGateway, spec.GatewayToken, spec.Snapshot.Model.Protocol, tracker.ready, tracker.requestProbe)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	defer relay.close()
	if err = validateDockerRelay(prepareCtx, tracker, relay, inspection.DockerGateway, probeImage); err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	lifecycle(emit, "relay_ready", "Docker-to-worker reachability mock passed without contacting a model")
	composePath := filepath.Join(attemptDir, "relay-compose.json")
	overlay, err := composeOverlay(spec, owner, inspection.DockerGateway, image, pinnedDockerfile)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	if err = writePrivate(composePath, overlay); err != nil {
		return infrastructureResult("could not persist private native compose overlay", resultPath), err
	}
	cfg, err := buildHarborJobConfig(spec, task, relay.URL, jobsDir, composePath, network)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	cfg.Environment.ForceBuild = pinnedDockerfile != ""
	if pinnedDockerfile != "" {
		var value map[string]any
		if json.Unmarshal(overlay, &value) == nil {
			main := value["services"].(map[string]any)["main"].(map[string]any)
			build := main["build"].(map[string]any)
			build["args"] = network.BuildArgs
			encoded, _ := json.MarshalIndent(value, "", "  ")
			if err := os.WriteFile(composePath, encoded, 0600); err != nil {
				return infrastructureResult("could not persist native build proxy arguments", resultPath), err
			}
		}
	}
	configPath := filepath.Join(attemptDir, "job-config.json")
	configBytes, _ := json.MarshalIndent(cfg, "", "  ")
	if err = writePrivate(configPath, configBytes); err != nil {
		return infrastructureResult("could not persist native JobConfig", resultPath), err
	}
	metadataHash := sha256.Sum256(metadataBytes)
	provenance = map[string]any{"snapshot_format_version": domain.SnapshotFormatVersion, "config_hash": spec.Snapshot.ConfigHash, "tools": inspection.Tools, "uv_version": inspectionToolVersion(inspection, "uv"), "harbor_version": inspectionToolVersion(inspection, "harbor"), "registry_url": spec.Snapshot.Suite.RegistryURL, "registry_kind": spec.Snapshot.RegistryKind, "registry_metadata_url": spec.Snapshot.RegistryMetadataURL, "dataset_version": spec.Snapshot.Suite.Version, "dataset_digest": spec.Snapshot.DatasetDigest, "dataset_revision": spec.Snapshot.DatasetRevision, "resolved_at": spec.Snapshot.ResolvedAt, "resolution_hash": spec.Snapshot.ResolutionHash, "task": task, "runtime": spec.Snapshot.Runtime, "task_metadata_sha256": hex.EncodeToString(metadataHash[:]), "native_image": image, "base_digests": baseDigests, "agent_installation": "harbor_default", "agent_schema_sha256": canonicalSchema(agent.Schema), "protocol": spec.Snapshot.Model.Protocol, "relay_ip": inspection.DockerGateway}
	setNetworkProvenance(provenance, spec.Snapshot)
	provenance["stage_ca_sha256"] = map[string]string{"source": network.Source.CASHA256, "agent": network.Agent.CASHA256, "verifier": network.Verifier.CASHA256}
	provenance["docker_network_observation"] = inspection.DockerNetwork
	provenance["build_network_applicable"] = pinnedDockerfile != ""
	provenanceBytes, _ := json.MarshalIndent(provenance, "", "  ")
	if err = replaceOwnedProvenance(provenancePath, provenanceBytes); err != nil {
		return infrastructureResult("could not persist execution source and tool observations", resultPath), err
	}
	networkPath := filepath.Join(attemptDir, "network-contract.json")
	networkBytes, _ := json.MarshalIndent(network, "", "  ")
	if err := writePrivate(networkPath, networkBytes); err != nil {
		return infrastructureResult("could not persist the credential-free phase network contract", resultPath), err
	}
	lifecycle(emit, "preflight", "Validating installed Harbor's native config and exact one-trial expansion")
	dryCtx, stopDry := context.WithTimeout(jobCtx, 120*time.Second)
	dryErr := validateNativeHarborJob(dryCtx, e, tools["harbor"], configPath, processEnv, network)
	stopDry()
	if dryErr != nil {
		return infrastructureResult(dryErr.Error(), resultPath), dryErr
	}
	lifecycle(emit, "running", "Starting one native Harbor trial with original verifier and no trial retries")
	command, err := harborNetworkRunCommand(e, tools["harbor"], configPath, networkPath)
	if err != nil {
		return infrastructureResult(err.Error(), resultPath), err
	}
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = processEnv
	cmd.Dir = attemptDir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	process, err := startOwned(jobCtx, cmd)
	if err != nil {
		return infrastructureResult("native Harbor CLI could not start", resultPath), err
	}
	observeCtx, stopObserve := context.WithCancel(jobCtx)
	observeDone := make(chan struct{})
	go tracker.observe(observeCtx, stopJob, observeDone)
	progress := time.NewTicker(time.Second)
	lastPhase := ""
	observed := false
	agentReady := tracker.ready
	waiting := true
	for waiting {
		select {
		case <-process.done:
			waiting = false
		case <-agentReady:
			if !observed {
				lifecycle(emit, "agent_ready", "Actual container agent version and task image identity verified before model forwarding")
				observed = true
			}
			agentReady = nil
		case <-progress.C:
			phase := harborProgress(jobDir, materialized.Name, materialized.TrialPrefix)
			if phase != "" && phase != lastPhase {
				lastPhase = phase
				lifecycle(emit, phase, progressMessage(phase))
			}
		}
	}
	progress.Stop()
	_ = process.wait()
	exitCode := process.exitCode()
	stopObserve()
	<-observeDone
	relay.close()
	readCtx, stopRead := context.WithTimeout(context.Background(), 2*time.Second)
	result, readErr := ReadHarborResultForTask(readCtx, jobDir, materialized.Name, task, exitCode, materialized.TrialPrefix)
	stopRead()
	result.AgentVersion, result.ImageDigests, err = tracker.snapshot()
	if err != nil {
		result.State = "infrastructure_error"
		result.Error = err.Error()
		return result, err
	}
	if ctx.Err() != nil {
		result.State = "cancelled"
		result.Error = "owned native Harbor attempt cancelled"
		return result, ctx.Err()
	}
	if jobCtx.Err() != nil {
		result.State = "timed_out"
		result.ExceptionType = "WorkerJobTimeout"
		result.Error = "owned native Harbor job exceeded its bounded lifetime"
		return result, nil
	}
	if readErr != nil {
		return result, readErr
	}
	lifecycle(emit, "finished", "Native trial finished; verifier reward and CLI exit are separate outcomes")
	return result, nil
}
func inspectionToolVersion(i domain.Inspection, name string) string {
	for _, tool := range i.Tools {
		if tool.Name == name {
			return tool.Version
		}
	}
	return ""
}
func validateDockerRelay(ctx context.Context, tracker *dockerTracker, relay *runningRelay, bindIP, image string) error {
	path := "/v1/chat/completions"
	switch tracker.spec.Snapshot.Model.Protocol {
	case domain.Responses:
		path = "/v1/responses"
	case domain.AnthropicMessages:
		path = "/v1/messages"
	}
	tokenName := "ROUTER_FORGE_REACHABILITY_TOKEN"
	python := `import os,urllib.request; t=os.environ['ROUTER_FORGE_REACHABILITY_TOKEN']; r=urllib.request.Request(os.environ['ROUTER_FORGE_REACHABILITY_URL'],data=b'{}',headers={'Authorization':'Bearer '+t,'X-Router-Forge-Probe':t,'Content-Type':'application/json'}); s=urllib.request.urlopen(r,timeout=10); assert s.status==204 and s.headers.get('X-Router-Forge-Probe')==t`
	script := `p=$(command -v python3 || command -v python || true); test -n "$p" || p=/opt/miniconda3/bin/python; exec "$p" -c ` + shellQuote(python)
	env := append(append([]string(nil), tracker.env...), tokenName+"="+relay.ProbeToken, "ROUTER_FORGE_REACHABILITY_URL="+relay.URL+path)
	args := []string{"run", "--rm", "--name", "rf-probe-" + strings.TrimPrefix(domain.NewID("p"), "p_"), "--network", "bridge", "--add-host", "router-forge-gateway:" + bindIP, "--label", "io.router-forge.owner=" + tracker.owner, "--label", "io.router-forge.run=" + tracker.spec.RunID, "--label", "io.router-forge.attempt=" + tracker.spec.AttemptID, "--env", tokenName, "--env", "ROUTER_FORGE_REACHABILITY_URL", "--entrypoint", "/bin/sh", image, "-c", script}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, err := tracker.runner(probeCtx, Command{Path: tracker.docker, Args: args}, env, "", nil, 16384); err != nil {
		return errors.New("Docker-to-worker relay mock failed; configure a locally bindable Docker bridge/host-gateway IP and proxy exclusions")
	}
	return nil
}
func harborProgress(jobDir, task string, nativePrefixes ...string) string {
	prefix := trialDirectoryPrefix(task)
	if len(nativePrefixes) == 1 {
		prefix = nativePrefixes[0]
	}
	if !simpleName.MatchString(prefix) {
		return ""
	}
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || !simpleName.MatchString(entry.Name()) || !strings.HasPrefix(entry.Name(), prefix+"__") {
			continue
		}
		data, err := readResultFile(filepath.Join(jobDir, entry.Name(), "result.json"))
		if err != nil {
			continue
		}
		var status struct {
			EnvironmentSetup *struct {
				StartedAt *string `json:"started_at"`
			} `json:"environment_setup"`
			AgentSetup *struct {
				StartedAt *string `json:"started_at"`
			} `json:"agent_setup"`
			AgentExecution *struct {
				StartedAt *string `json:"started_at"`
			} `json:"agent_execution"`
			Verifier *struct {
				StartedAt *string `json:"started_at"`
			} `json:"verifier"`
		}
		if json.Unmarshal(data, &status) != nil {
			continue
		}
		switch {
		case status.Verifier != nil && status.Verifier.StartedAt != nil:
			return "verifying"
		case status.AgentExecution != nil && status.AgentExecution.StartedAt != nil:
			return "executing_agent"
		case status.AgentSetup != nil && status.AgentSetup.StartedAt != nil:
			return "installing_agent"
		case status.EnvironmentSetup != nil && status.EnvironmentSetup.StartedAt != nil:
			return "starting_environment"
		}
	}
	return ""
}
func progressMessage(phase string) string {
	switch phase {
	case "verifying":
		return "Original native verifier is running"
	case "executing_agent":
		return "Selected native container agent is executing"
	case "installing_agent":
		return "Harbor is using its default agent installation inside the task container; independent CLI observation is still required"
	case "starting_environment":
		return "Harbor is preparing the original task environment"
	}
	return "Native lifecycle update"
}

var _ WorkerBackend = (*nativeWorker)(nil)
var _ http.RoundTripper = workerRoundTripper{}

package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func fixtureSpec() domain.ExecutionSpec {
	agent, _ := ParseAgentCapabilities("opencode", []byte(`{"agent":"opencode","supports_model_connection":true,"supports_overriding_base_url":true,"supported_api_formats":["openai_chat_completions","anthropic_messages"]}`), []byte(`{"type":"object","properties":{"disable_web_search":{"type":"boolean"}}}`))
	suite := domain.SWEBenchVerified()
	suite.Version, suite.RegistryURL, suite.TaskCount = "native-fixture-version", "https://native.fixture.test/registry.json", 1
	task := domain.BenchmarkTask{Name: "fixture__task-1", Kind: "git", Path: "arbitrary-layout/fixture__task-1", GitURL: "https://code.fixture.test/tasks.git", GitCommit: strings.Repeat("a", 40)}
	resolved := domain.ResolvedBenchmark{Suite: suite, RegistryKind: "json", ResolvedAt: domain.Now(), Tasks: []domain.BenchmarkTask{task}}
	resolved.ResolutionHash, _ = resolved.Hash()
	spec := domain.ExecutionSpec{RunID: "run_fixture", TaskID: "task_fixture", AttemptID: "attempt_fixture", JobName: "rf_fixture", GatewayToken: "fixture-lease-only", Snapshot: domain.RunSnapshot{Spec: domain.RunSpec{EnvironmentID: "env_fixture", ModelID: "model_fixture", SuiteID: "swebench-verified", TaskNames: []string{task.Name}, Agent: "opencode", TimeoutSec: 60, SetupTimeoutSec: 30, MaxRequests: 2, MaxOutputTokens: 100, Concurrency: 1, Attempts: 1, ConfirmPaid: true}, Environment: domain.Environment{ID: "env_fixture", Type: "wsl", Distribution: "Ubuntu"}, Model: domain.Model{ID: "model_fixture", ProviderID: "provider_fixture", Name: "fixture-model", Protocol: domain.ChatCompletions}, Provider: domain.Provider{ID: "provider_fixture"}, Suite: suite, HarborVersion: "9.8.7", UVVersion: "8.7.6", Agent: agent, FormatVersion: domain.SnapshotFormatVersion, RegistryKind: resolved.RegistryKind, ResolvedAt: resolved.ResolvedAt, ResolutionHash: resolved.ResolutionHash, ResolvedTasks: resolved.Tasks}}
	sealFixtureSpec(&spec)
	return spec
}
func sealFixtureSpec(spec *domain.ExecutionSpec) {
	spec.Snapshot.ConfigHash, _ = spec.Snapshot.ApprovalHash()
}
func domainTask(spec domain.ExecutionSpec) domain.BenchmarkTask {
	return spec.Snapshot.ResolvedTasks[0]
}

func TestNativeJobConfigUsesRecordedTaskDefaultAgentProtocolAndOriginalVerifier(t *testing.T) {
	spec := fixtureSpec()
	task := domainTask(spec)
	cfg, err := BuildHarborJobConfig(spec, task, "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cfg)
	var got map[string]json.RawMessage
	_ = json.Unmarshal(data, &got)
	if string(got["n_attempts"]) != "1" || string(got["n_concurrent_trials"]) != "1" || !strings.Contains(string(got["retry"]), `"max_retries":0`) {
		t.Fatalf("run limits: %s", data)
	}
	if cfg.Tasks[0].GitCommitID != task.GitCommit || cfg.Tasks[0].Path != task.Path || len(cfg.Datasets) != 0 {
		t.Fatal("exact selected source was substituted or expanded")
	}
	if strings.Contains(string(data), spec.GatewayToken) || !strings.Contains(string(got["agents"]), `"model_api_format":"openai_chat_completions"`) || strings.Contains(string(got["agents"]), `"version":`) {
		t.Fatalf("native routing or default agent installation: %s", data)
	}
	if strings.Contains(string(got["verifier"]), "import_path") || !strings.Contains(string(got["verifier"]), `"disable":false`) {
		t.Fatal("original verifier replaced/disabled")
	}
}
func TestOpenCodeSelectorKeepsApprovedWireModelLiteral(t *testing.T) {
	for _, model := range []string{"fixture-model", "org/model:custom", "router-forge/existing-model"} {
		for _, protocol := range []domain.Protocol{domain.ChatCompletions, domain.Responses, domain.AnthropicMessages} {
			spec := fixtureSpec()
			spec.Snapshot.Model.Name, spec.Snapshot.Model.Protocol = model, protocol
			spec.Snapshot.Agent.SupportedProtocols = []domain.Protocol{protocol}
			sealFixtureSpec(&spec)
			cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json")
			if err != nil {
				t.Fatal(err)
			}
			agent := cfg.Agents[0]
			if agent.ModelName != "router-forge/"+model || agent.BaseURL != "http://router-forge-gateway:4567/v1" || !agent.SkipModelParsing || agent.ModelAPIFormat != harborAPIFormat(protocol) || agent.ModelAPIKeyName != attemptKeyName || spec.Snapshot.Model.Name != model {
				t.Fatalf("native provider selector changed approved wire routing: %+v", agent)
			}
		}
	}
	for _, name := range []string{"codex", "claude-code"} {
		spec := fixtureSpec()
		spec.Snapshot.Agent.Name, spec.Snapshot.Spec.Agent = name, name
		sealFixtureSpec(&spec)
		cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json")
		if err != nil || cfg.Agents[0].ModelName != spec.Snapshot.Model.Name {
			t.Fatal("another native agent model selector was changed", err)
		}
	}
}

func TestDirectGitTaskConfigNeverActivatesPackageSource(t *testing.T) {
	spec := fixtureSpec()
	cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	var task map[string]json.RawMessage
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	if _, ok := task["name"]; ok {
		t.Fatal("Harbor direct Git task cannot also set package name")
	}
	if cfg.Tasks[0].DownloadDir != "/home/runner/work/task-cache" {
		t.Fatal("Linux task cache path was mixed with Windows separators")
	}
}
func TestExecutionRefusesPaidWorkWithoutExactBoundedFrozenSpec(t *testing.T) {
	for _, mutate := range []func(*domain.ExecutionSpec){
		func(s *domain.ExecutionSpec) { s.Snapshot.Spec.ConfirmPaid = false },
		func(s *domain.ExecutionSpec) { s.Snapshot.Spec.AgentVersion = "latest" },
		func(s *domain.ExecutionSpec) { s.Snapshot.Spec.Concurrency = 2 },
		func(s *domain.ExecutionSpec) { s.Snapshot.Model.Protocol = domain.Responses },
		func(s *domain.ExecutionSpec) { s.Snapshot.Suite.TaskCommit = "main" },
		func(s *domain.ExecutionSpec) {
			s.Snapshot.Spec.AgentKwargs = map[string]json.RawMessage{"opencode_config": json.RawMessage(`{"provider":{"api_key":"not-allowed"}}`)}
		},
		func(s *domain.ExecutionSpec) {
			s.Snapshot.Spec.AgentKwargs = map[string]json.RawMessage{"version": json.RawMessage(`"10.0.0"`)}
		},
	} {
		spec := fixtureSpec()
		mutate(&spec)
		if err := ValidateExecutionSpec(spec); err == nil {
			t.Fatalf("unsafe execution accepted: %#v", spec.Snapshot.Spec)
		}
	}
}
func TestInstalledVersionsAndCurrentAgentSchemaAreInformational(t *testing.T) {
	spec := fixtureSpec()
	spec.Snapshot.HarborVersion, spec.Snapshot.UVVersion = "custom development build", "nightly"
	spec.Snapshot.Agent.Version = "new host hint"
	spec.Snapshot.Agent.SupportsVersion = false
	if err := ValidateExecutionSpec(spec); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.2.3", "latest", "nightly"} {
		spec := fixtureSpec()
		spec.Snapshot.Spec.AgentVersion = version
		if err := ValidateExecutionSpec(spec); err == nil {
			t.Errorf("explicit agent installation request %q accepted", version)
		}
	}
}
func TestNativeJobConfigFixtureMatchesGeneratedConfig(t *testing.T) {
	spec := fixtureSpec()
	spec.Snapshot.Environment.CACertPath = "/home/fixture/ca-profile.pem"
	spec.Snapshot.Environment.DockerHostGateway = "172.17.0.1"
	sealFixtureSpec(&spec)
	root := "/home/fixture/.local/share/router-forge/work/run_fixture/attempt_fixture"
	cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", root+"/jobs", root+"/relay-compose.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "benchmarks", "harbor", "fixtures", "job-config.json")
	if os.Getenv("ROUTER_FORGE_REFRESH_SYNTHETIC_CONFIG") == "1" {
		if err := os.WriteFile(fixture, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if json.Unmarshal(expected, &want) != nil || json.Unmarshal(data, &got) != nil {
		t.Fatal("invalid synthetic fixture JSON")
	}
	canonicalWant, _ := json.Marshal(want)
	canonicalGot, _ := json.Marshal(got)
	if string(canonicalWant) != string(canonicalGot) {
		t.Fatal("synthetic full native JobConfig fixture does not match generated configuration")
	}
}
func TestPackageJobConfigKeepsExactDigestWithoutInventingGitCommit(t *testing.T) {
	spec := fixtureSpec()
	task := domain.BenchmarkTask{Name: "swe-bench/fixture__task-1", Kind: "package", PackageName: "swe-bench/fixture__task-1", Ref: "sha256:" + strings.Repeat("c", 64)}
	spec.Snapshot.Spec.TaskNames = []string{task.Name}
	spec.Snapshot.ResolvedTasks = []domain.BenchmarkTask{task}
	spec.Snapshot.RegistryKind = "package"
	spec.Snapshot.DatasetDigest = "sha256:" + strings.Repeat("d", 64)
	spec.Snapshot.Suite.Version = spec.Snapshot.DatasetDigest
	sealFixtureSpec(&spec)
	cfg, err := BuildHarborJobConfig(spec, task, "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 || len(cfg.Datasets) != 0 || cfg.Tasks[0].Name != task.PackageName || cfg.Tasks[0].Ref != task.Ref || cfg.Tasks[0].Path != "" || cfg.Tasks[0].GitCommitID != "" || cfg.Tasks[0].GitURL != "" || cfg.Agents[0].Kwargs["version"] != nil {
		t.Fatalf("package source or default agent installation changed: %+v", cfg)
	}
	task.Name, task.PackageName = "different/fixture__task-1", "different/fixture__task-1"
	if _, err := BuildHarborJobConfig(spec, task, "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/relay-compose.json"); err == nil {
		t.Fatal("another namespace's same basename was accepted")
	}
}

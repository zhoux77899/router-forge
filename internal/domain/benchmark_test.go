package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func snapshotFixture(t *testing.T) RunSnapshot {
	t.Helper()
	suite := SWEBenchVerified()
	suite.Version, suite.RegistryURL, suite.TaskCount = "2.7", "https://registry.example/registry.json", 17
	s := RunSnapshot{
		FormatVersion: SnapshotFormatVersion, Suite: suite, RegistryKind: "json", ResolvedAt: Now(), ResolutionHash: strings.Repeat("b", 64),
		Spec:        RunSpec{EnvironmentID: "env", ModelID: "model", SuiteID: suite.ID, TaskNames: []string{"task"}, Agent: "opencode", Concurrency: 1, Attempts: 1, TimeoutSec: 60, SetupTimeoutSec: 60, MaxRequests: 2, MaxOutputTokens: 32},
		Environment: Environment{ID: "env"}, Model: Model{ID: "model", Name: "model", ProviderID: "provider", Protocol: ChatCompletions}, Provider: Provider{ID: "provider", KeyRevision: "revision-3"},
		ResolvedTasks: []BenchmarkTask{{Name: "task", Kind: "git", Path: "another-layout/task", GitURL: "https://github.com/example/tasks.git", GitCommit: strings.Repeat("a", 40)}},
	}
	execution := networkFixture(NetworkEnvironment, "env", "execution")
	modelNetwork := networkFixture(NetworkDesktop, "", "model")
	s.Network = &NetworkSnapshot{Source: execution, Build: execution, Agent: execution, Verifier: execution, Model: modelNetwork}
	s.Environment.Networks = &EnvironmentNetworks{SourceProfileID: execution.ID, BuildProfileID: execution.ID, AgentProfileID: execution.ID, VerifierProfileID: execution.ID}
	s.Environment.DockerGatewayMode = "detect"
	s.Provider.NetworkProfileID = modelNetwork.ID
	var err error
	s.ConfigHash, err = s.ApprovalHash()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStableSuiteDoesNotDeclareVersionsOrSources(t *testing.T) {
	s := SWEBenchVerified()
	if s.Dataset != "swe-bench/swe-bench-verified" || s.Version != "" || s.RegistryURL != "" || s.TaskRepository != "" || s.TaskCommit != "" || s.TaskCount != 0 || s.SuggestedTask != "" {
		t.Fatalf("stable descriptor contains resolution defaults: %+v", s)
	}
}

func TestApprovalHashSeparatesSourceIdentityFromObservations(t *testing.T) {
	s := snapshotFixture(t)
	if err := s.ValidateSources(); err != nil {
		t.Fatal(err)
	}
	s.HarborVersion, s.UVVersion, s.ResolvedAt = "9.8.7", "nightly", Now()
	s.Spec.ConfirmPaid = true
	s.Agent.Version, s.Agent.Schema = "new host hint", json.RawMessage(`{"type":"object","properties":{}}`)
	s.Runtime = &RuntimeSnapshot{ObservedAt: Now(), AgentVersion: "2.0.0"}
	if err := s.ValidateSources(); err != nil {
		t.Fatalf("runtime observation invalidated approval: %v", err)
	}
	for _, change := range []func(*RunSnapshot){
		func(s *RunSnapshot) { s.Spec.MaxRequests++ },
		func(s *RunSnapshot) { s.Provider.KeyRevision = "another-revision" },
		func(s *RunSnapshot) { s.Model.Name = "another-model" },
		func(s *RunSnapshot) { s.Environment.WorkDir = "/another/workdir" },
		func(s *RunSnapshot) { s.Suite.Version = "3.0" },
		func(s *RunSnapshot) { s.Suite.RegistryURL = "https://different.example/registry.json" },
		func(s *RunSnapshot) { s.ResolvedTasks[0].GitCommit = strings.Repeat("c", 40) },
	} {
		copy := snapshotFixture(t)
		change(&copy)
		if copy.ValidateSources() == nil {
			t.Fatal("approved source or spending/identity boundary was mutable")
		}
	}
}

func TestTaskSourcesValidateSyntaxNotFrozenValues(t *testing.T) {
	git := snapshotFixture(t).ResolvedTasks[0]
	pkg := BenchmarkTask{Name: "swe-bench/task", Kind: "package", PackageName: "swe-bench/task", Ref: "sha256:" + strings.Repeat("d", 64), Revision: "42"}
	for _, task := range []BenchmarkTask{git, pkg} {
		if err := task.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*BenchmarkTask){
		func(t *BenchmarkTask) { t.GitCommit = "main" },
		func(t *BenchmarkTask) { t.GitURL = "https://user:secret@example.com/tasks" },
		func(t *BenchmarkTask) { t.Path = "../task" },
		func(t *BenchmarkTask) { t.Path = "tasks/../task" },
		func(t *BenchmarkTask) { t.Path = "C:\\tasks\\task" },
		func(t *BenchmarkTask) { t.PackageName = "swe-bench/task" },
	} {
		copy := git
		change(&copy)
		if copy.Validate() == nil {
			t.Fatal("unsafe or mixed task source accepted")
		}
	}
	pkg.Ref = "latest"
	if pkg.Validate() == nil {
		t.Fatal("floating package ref accepted")
	}
	for _, name := range []string{"../task", "org/../task", "org/name/another", "org//task", "org/..", "task\n"} {
		if ValidTaskName(name) {
			t.Fatal("unsafe task name", name)
		}
	}
}

func TestLegacySnapshotsRemainReadableButCannotStart(t *testing.T) {
	var old RunSnapshot
	if err := json.Unmarshal([]byte(`{"spec":{"agent_version":"1.19.4"},"harbor_version":"0.25.0","suite":{"version":"1.0"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.HarborVersion != "0.25.0" || old.Spec.AgentVersion != "1.19.4" || old.Runtime != nil || old.FormatVersion != 0 {
		t.Fatal("legacy evidence was changed or fabricated")
	}
	if old.ValidateSources() == nil {
		t.Fatal("legacy draft was startable")
	}
}

func TestResolvedCatalogHashExcludesProbeTime(t *testing.T) {
	s := snapshotFixture(t)
	s.Suite.TaskCount = 1
	r := ResolvedBenchmark{Suite: s.Suite, RegistryKind: s.RegistryKind, ResolvedAt: Now(), Tasks: s.ResolvedTasks}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	before, _ := r.Hash()
	r.ResolvedAt, r.ResolutionHash = "2030-01-01T00:00:00Z", before
	after, _ := r.Hash()
	if before != after {
		t.Fatal("resolution timestamp changed source identity")
	}
	r.Tasks[0].GitCommit = strings.Repeat("f", 40)
	changed, _ := r.Hash()
	if changed == before {
		t.Fatal("task commit was excluded from source identity")
	}
}

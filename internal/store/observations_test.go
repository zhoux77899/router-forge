package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func observedRunFixture(t *testing.T, s *Store) (domain.Run, []domain.ExecutionObservation) {
	t.Helper()
	ctx := context.Background()
	run := testRun(t, s)
	snapshot := run.Snapshot
	snapshot.Spec = domain.RunSpec{EnvironmentID: snapshot.Environment.ID, ModelID: snapshot.Model.ID, SuiteID: snapshot.Suite.ID, TaskNames: []string{"django__django-13741"}, Agent: "opencode", TimeoutSec: 60, SetupTimeoutSec: 60, MaxRequests: 2, MaxOutputTokens: 128, Concurrency: 1, Attempts: 1}
	snapshot.Suite.Version, snapshot.Suite.RegistryURL, snapshot.Suite.TaskCount = "current-fixture", "https://registry.fixture.test/catalog.json", 1
	snapshot.ResolvedTasks = []domain.BenchmarkTask{{Name: snapshot.Spec.TaskNames[0], Kind: "git", Path: "tasks/django__django-13741", GitURL: "https://code.fixture.test/tasks.git", GitCommit: strings.Repeat("a", 40)}}
	snapshot.FormatVersion, snapshot.RegistryKind, snapshot.ResolvedAt = domain.SnapshotFormatVersion, "json", domain.Now()
	resolved := domain.ResolvedBenchmark{Suite: snapshot.Suite, RegistryKind: snapshot.RegistryKind, ResolvedAt: snapshot.ResolvedAt, Tasks: snapshot.ResolvedTasks}
	snapshot.ResolutionHash, _ = resolved.Hash()
	execution, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: storeNetworkFixture(domain.NetworkEnvironment, snapshot.Environment.ID)})
	if err != nil {
		t.Fatal(err)
	}
	modelNetwork, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: storeNetworkFixture(domain.NetworkDesktop, "")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Network = &domain.NetworkSnapshot{Source: execution, Build: execution, Agent: execution, Verifier: execution, Model: modelNetwork}
	snapshot.Environment.Networks = &domain.EnvironmentNetworks{SourceProfileID: execution.ID, BuildProfileID: execution.ID, AgentProfileID: execution.ID, VerifierProfileID: execution.ID}
	snapshot.Environment.DockerGatewayMode = "detect"
	snapshot.Provider.NetworkProfileID = modelNetwork.ID
	snapshot.ConfigHash, _ = snapshot.ApprovalHash()
	data, _ := json.Marshal(snapshot)
	if _, err := s.DB.ExecContext(ctx, "UPDATE evaluation_runs SET snapshot_json=? WHERE id=?", string(data), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.TransitionRun(ctx, run.ID, "draft", "preparing", ""); err != nil {
		t.Fatal(err)
	}
	attempt := run.Tasks[0].Attempts[0]
	attempt.State = "preparing"
	if err := s.SaveAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	run.Snapshot = snapshot
	return run, []domain.ExecutionObservation{
		{Kind: "tools", Timestamp: domain.Now(), WorkDir: filepath.Join(t.TempDir(), "work"), Tools: []domain.ToolStatus{{Name: "uv", Path: "fixture-uv", Available: true, Version: "8.7.6", VersionOutput: "uv 8.7.6"}, {Name: "harbor", Path: "fixture-harbor", Available: true, Version: "9.8.7", VersionOutput: "Harbor 9.8.7"}}},
		{Kind: "task", Timestamp: domain.Now(), Task: &snapshot.ResolvedTasks[0], RegistryURL: snapshot.Suite.RegistryURL, TaskPath: "/private/fixture/task", TaskName: snapshot.Spec.TaskNames[0], TrialPrefix: snapshot.Spec.TaskNames[0]},
		{Kind: "agent", Timestamp: domain.Now(), AgentVersion: "custom-development-build", AgentVersionOutput: "opencode custom-development-build"},
	}
}

func TestExecutionObservationsAreAtomicScopedAndImmutable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run, observations := observedRunFixture(t, s)
	attempt := run.Tasks[0].Attempts[0]
	if err := s.RecordExecutionObservation(ctx, run.ID, "foreign-task", attempt.ID, observations[0]); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign observation: %v", err)
	}
	if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observations[2]); err == nil {
		t.Fatal("agent observation was admitted before tools and task")
	}
	for _, observation := range observations {
		if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observation); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observation); err == nil {
			t.Fatal("recorded observation was replaced")
		}
	}
	current, err := s.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Snapshot.ConfigHash != run.Snapshot.ConfigHash || current.Snapshot.Runtime.AgentVersion != observations[2].AgentVersion || current.Tasks[0].Attempts[0].AgentVersion != observations[2].AgentVersion || current.Snapshot.HarborVersion != "9.8.7" {
		t.Fatal("observation changed approval or lost independently observed evidence")
	}
}

func TestAgentObservationRollbackNeverAcknowledgesPartialCommit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run, observations := observedRunFixture(t, s)
	attempt := run.Tasks[0].Attempts[0]
	for _, observation := range observations[:2] {
		if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observation); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE TRIGGER reject_observed_agent BEFORE UPDATE OF result_json ON evaluation_attempts BEGIN SELECT RAISE(ABORT, 'fixture persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observations[2]); err == nil {
		t.Fatal("failed transaction was acknowledged")
	}
	current, err := s.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Snapshot.Runtime.AgentVersion != "" || current.Tasks[0].Attempts[0].AgentVersion != "" || current.Snapshot.Runtime.TaskPath == "" {
		t.Fatal("failed agent transaction partially committed or discarded prior tools/task evidence")
	}
}

func TestCancelledAndRecoveredRunsRetainPartialObservations(t *testing.T) {
	for _, state := range []string{"cancelled", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			run, observations := observedRunFixture(t, s)
			attempt := run.Tasks[0].Attempts[0]
			if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observations[0]); err != nil {
				t.Fatal(err)
			}
			var err error
			if state == "interrupted" {
				err = s.RecoverInterrupted(ctx)
			} else {
				err = s.TransitionRun(ctx, run.ID, "preparing", state, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RecordExecutionObservation(ctx, run.ID, attempt.TaskID, attempt.ID, observations[1]); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("late observation: %v", err)
			}
			current, err := s.Run(ctx, run.ID)
			if err != nil || current.Snapshot.Runtime == nil || len(current.Snapshot.Runtime.Tools) != 2 || current.Snapshot.Runtime.AgentVersion != "" || current.Snapshot.ConfigHash != run.Snapshot.ConfigHash {
				t.Fatalf("partial evidence was lost: %v", err)
			}
		})
	}
}

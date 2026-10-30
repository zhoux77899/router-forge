package control

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestDraftSourceSurvivesUpstreamDriftWhileRuntimeToolsCanUpgrade(t *testing.T) {
	f := setup(t, false)
	draft := f.draft(t)
	f.exec.taskCommit = strings.Repeat("b", 40)
	f.exec.harborVersion = "10.4.2"
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	result[domain.Run](t, code, body)
	run := waitRun(t, f, draft.ID)
	if f.exec.resolveCalls.Load() != 1 || run.Snapshot.ResolvedTasks[0].GitCommit != strings.Repeat("a", 40) || run.Snapshot.ConfigHash != draft.Snapshot.ConfigHash {
		t.Fatal("start re-resolved or altered the approved source")
	}
	if run.Snapshot.HarborVersion != "10.4.2" || run.Snapshot.Runtime == nil || run.Snapshot.Runtime.AgentVersion != "2.4.0" || run.Tasks[0].Attempts[0].AgentVersion != "2.4.0" {
		t.Fatal("runtime tools or independent container agent version were not persisted")
	}
	for _, tool := range run.Snapshot.PreflightTools {
		if tool.Name == "harbor" && tool.Version != "9.8.7" {
			t.Fatal("start rewrote the preflight hint")
		}
	}
}

func TestLegacyDraftRemainsReadableButCannotStart(t *testing.T) {
	f := setup(t, false)
	draft := f.draft(t)
	snapshot := draft.Snapshot
	snapshot.FormatVersion = 0
	snapshot.Spec.AgentVersion = "1.19.4"
	data, _ := json.Marshal(snapshot)
	if _, err := f.store.DB.ExecContext(context.Background(), "UPDATE evaluation_runs SET snapshot_json=? WHERE id=?", string(data), draft.ID); err != nil {
		t.Fatal(err)
	}
	code, body := f.request(t, "GET", "/api/runs/"+draft.ID, nil, false)
	legacy := result[domain.Run](t, code, body)
	if legacy.Snapshot.Spec.AgentVersion != "1.19.4" {
		t.Fatal("legacy report was rewritten")
	}
	code, _ = f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	if code != http.StatusConflict || f.exec.calls.Load() != 0 || f.calls.Load() != 0 {
		t.Fatal("legacy frozen draft was silently migrated or executed")
	}
}

func TestUnobservedAgentCannotForwardInferenceOrComplete(t *testing.T) {
	f := setup(t, false)
	f.exec.omitAgent = true
	draft := f.draft(t)
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	result[domain.Run](t, code, body)
	run := waitRun(t, f, draft.ID)
	if run.State != "failed" || f.calls.Load() != 0 || run.Snapshot.Runtime == nil || run.Snapshot.Runtime.TaskPath == "" || run.Snapshot.Runtime.AgentVersion != "" {
		t.Fatal("missing durable agent observation authorized inference or discarded partial evidence")
	}
}

func TestAgentPersistenceFailureLeavesGateClosedAndPriorEvidenceIntact(t *testing.T) {
	f := setup(t, false)
	draft := f.draft(t)
	if _, err := f.store.DB.ExecContext(context.Background(), `CREATE TRIGGER reject_agent_receipt BEFORE UPDATE OF result_json ON evaluation_attempts WHEN COALESCE(json_extract(NEW.result_json, '$.agent_version'), '') != '' BEGIN SELECT RAISE(ABORT, 'fixture durable storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	result[domain.Run](t, code, body)
	run := waitRun(t, f, draft.ID)
	if run.State != "failed" || f.calls.Load() != 0 || run.Snapshot.Runtime == nil || run.Snapshot.Runtime.TaskPath == "" || run.Snapshot.Runtime.AgentVersion != "" || run.Tasks[0].Attempts[0].AgentVersion != "" {
		t.Fatal("failed receipt partially persisted or admitted provider use")
	}
}

func TestReadonlyCannotResolveProtectedBenchmarkSources(t *testing.T) {
	f := setup(t, false)
	code, _ := f.request(t, "GET", "/api/benchmarks/swebench-verified/resolve?environment_id=fixture", nil, false)
	if code != http.StatusUnauthorized || f.exec.resolveCalls.Load() != 0 {
		t.Fatal("readonly client gained source resolution or environment access")
	}
}

package control

import (
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestNativeContainerAdapterDoesNotRequireHostAgentInstallation(t *testing.T) {
	f := setup(t, false)
	f.exec.hostAbsent = true
	draft := f.draft(t)
	if draft.State != "draft" || draft.Snapshot.Agent.Installed || !draft.Snapshot.Agent.HarborSupported || f.calls.Load() != 0 {
		t.Fatal("host discovery was mistaken for container installation readiness")
	}
}

func TestCancellingDraftDoesNotLeavePendingAttempt(t *testing.T) {
	f := setup(t, false)
	draft := f.draft(t)
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/cancel", map[string]any{}, true)
	run := result[domain.Run](t, code, body)
	attempt := run.Tasks[0].Attempts[0]
	if run.State != "cancelled" || run.Tasks[0].State != "cancelled" || attempt.State != "cancelled" || attempt.FinishedAt == nil || attempt.StartedAt != nil || f.calls.Load() != 0 || f.exec.calls.Load() != 0 {
		t.Fatalf("draft cancel failed: %+v", run)
	}
	code, _ = f.request(t, "POST", "/api/runs/"+run.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	if code != 409 || f.calls.Load() != 0 || f.exec.calls.Load() != 0 {
		t.Fatal("cancelled draft started")
	}
}

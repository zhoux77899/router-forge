package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestDraftCancellationTerminatesChildrenWithoutStartingThem(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	run, err := s.CreateRun(ctx, domain.RunSnapshot{Spec: domain.RunSpec{TaskNames: []string{"one"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.TransitionRun(ctx, run.ID, "draft", "cancelled", ""); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := updated.Tasks[0].Attempts[0]
	if updated.State != "cancelled" || updated.FinishedAt == nil || updated.StartedAt != nil || updated.Tasks[0].State != "cancelled" || attempt.State != "cancelled" || attempt.FinishedAt == nil || attempt.StartedAt != nil || attempt.Reward != nil || attempt.ExitCode != nil {
		t.Fatalf("draft cancellation fabricated execution or left active children: %+v / %+v", updated, attempt)
	}
	if err = s.TransitionRun(ctx, run.ID, "draft", "preparing", ""); err != domain.ErrConflict {
		t.Fatalf("cancelled draft admitted: %v", err)
	}
}

func TestRunFinalizationPreservesIndependentAttemptResult(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	run, err := s.CreateRun(ctx, domain.RunSnapshot{Spec: domain.RunSpec{TaskNames: []string{"one"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.TransitionRun(ctx, run.ID, "draft", "preparing", ""); err != nil {
		t.Fatal(err)
	}
	attempt := run.Tasks[0].Attempts[0]
	attempt.State = "failed"
	attempt.ExceptionType = "AgentTimeoutError"
	reward := 0.0
	attempt.Reward = &reward
	finished := domain.Now()
	attempt.FinishedAt = &finished
	if err = s.SaveAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	if err = s.TransitionRun(ctx, run.ID, "preparing", "failed", "Independent timeout"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := updated.Tasks[0].Attempts[0]
	if got.ExceptionType != "AgentTimeoutError" || got.Reward == nil || *got.Reward != 0 || got.FinishedAt == nil || *got.FinishedAt != finished {
		t.Fatalf("finalization overwrote verifier result: %+v", got)
	}
}

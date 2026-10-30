package store

import (
	"context"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestRepeatedWorkerEventIsIdempotent(t *testing.T) {
	s := testStore(t)
	r := testRun(t, s)
	ctx := context.Background()
	e := domain.JobEvent{RunID: r.ID, AttemptID: r.Tasks[0].Attempts[0].ID, Kind: "verifier", Message: "Completed", Timestamp: domain.Now()}
	if err := s.AddEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, r.ID, 0)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
}

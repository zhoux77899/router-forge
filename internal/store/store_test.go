package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "app"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPlaintextCredentialsAreNotPublicAndRevisionIsFrozen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	key := "unit-only-original-secret"
	p, err := s.SaveProvider(ctx, domain.ProviderInput{Name: "Mock", BaseURL: "https://example.test/v1", APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	old := p.KeyRevision
	for _, value := range []any{p, domain.RunSnapshot{Provider: p}} {
		b, _ := json.Marshal(value)
		if strings.Contains(string(b), key) || strings.Contains(string(b), "api_key") {
			t.Fatal("key appeared in public JSON")
		}
	}
	stored, err := s.Credential(ctx, p.ID, old)
	if err != nil || stored != key {
		t.Fatal(stored, err)
	}
	key2 := "unit-only-rotated-secret"
	p, err = s.SaveProvider(ctx, domain.ProviderInput{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: &key2})
	if err != nil {
		t.Fatal(err)
	}
	if p.KeyRevision == old {
		t.Fatal("credential rotation did not produce a new immutable revision")
	}
	stored, err = s.Credential(ctx, p.ID, old)
	if err != nil || stored != key {
		t.Fatal("old run credential ref changed")
	}
	p, err = s.SaveProvider(ctx, domain.ProviderInput{ID: p.ID, Name: "Renamed", BaseURL: p.BaseURL})
	if err != nil || !p.HasKey {
		t.Fatal("omitting key should retain it", err)
	}
	key = ""
	p, err = s.SaveProvider(ctx, domain.ProviderInput{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: &key})
	if err != nil || p.HasKey {
		t.Fatal("explicit empty key did not clear active credential", err)
	}
}

func testRun(t *testing.T, s *Store) domain.Run {
	t.Helper()
	ctx := context.Background()
	key := "unit-only-secret"
	p, err := s.SaveProvider(ctx, domain.ProviderInput{Name: "Mock", BaseURL: "https://example.test/v1", APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.SaveModel(ctx, domain.Model{ProviderID: p.ID, Name: "mock-model", Protocol: domain.ChatCompletions})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveEnvironment(ctx, domain.Environment{Name: "Local", Type: "local"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRun(ctx, domain.RunSnapshot{Environment: e, Model: m, Provider: p, Suite: domain.SWEBenchVerified(), Spec: domain.RunSpec{TaskNames: []string{"django__django-13741"}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLedgerPersistenceScopeAndExactCurrencyTotals(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := testRun(t, s)
	a := run.Tasks[0].Attempts[0]
	input, output := int64(7), int64(3)
	for i, currency := range []string{"USD", "USD", "CNY"} {
		r := domain.RequestRecord{ID: domain.NewID("request"), RunID: run.ID, TaskID: a.TaskID, AttemptID: a.ID, ProviderID: run.Snapshot.Provider.ID, ModelID: run.Snapshot.Model.ID, Protocol: domain.ChatCompletions, Kind: "inference", StartedAt: domain.Now(), State: "running", Usage: domain.Usage{Source: "unknown", Completeness: "unknown"}, CostSource: "unknown"}
		if err := s.BeginRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
		amount := []string{"0.000000000000000001", "0.000000000000000002", "0.2"}[i]
		now := domain.Now()
		r.State = "completed"
		r.FinishedAt = &now
		r.Usage = domain.Usage{InputTokens: &input, OutputTokens: &output, Source: "provider", Completeness: "complete"}
		r.CostAmount = &amount
		r.Currency = &currency
		r.CostSource = "estimated"
		if err := s.EndRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := s.RequestSummary(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequestCount != 3 || summary.InputTokens != 21 || len(summary.Costs) != 2 || summary.Costs[1].Currency != "USD" || summary.Costs[1].Amount != "0.000000000000000003" {
		t.Fatalf("%+v", summary)
	}
	page, err := s.Requests(ctx, run.ID, "", 2, 0)
	if err != nil || page.Total != 3 || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	bad := page.Items[0]
	bad.ID = domain.NewID("request")
	bad.TaskID = "unrelated"
	if s.BeginRequest(ctx, bad) == nil {
		t.Fatal("accepted mismatched task scope")
	}
}

func TestRecoveryDoesNotRetryOrReportSolved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := testRun(t, s)
	if err := s.TransitionRun(ctx, run.ID, "draft", "preparing", ""); err != nil {
		t.Fatal(err)
	}
	a := run.Tasks[0].Attempts[0]
	a.State = "running"
	if err := s.SaveAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := s.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "interrupted" || r.Tasks[0].Attempts[0].Reward != nil || r.Tasks[0].Attempts[0].State != "interrupted" {
		t.Fatalf("%+v", r)
	}
	if err := s.TransitionRun(ctx, run.ID, "draft", "preparing", ""); err == nil {
		t.Fatal("restarted recovered paid run")
	}
}

func TestPrivateAtomicFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "state.json")
	for _, value := range []string{"first", "second"} {
		if err := WritePrivateFile(path, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "second" {
		t.Fatal(string(b), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}

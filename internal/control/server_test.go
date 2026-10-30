package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
	"github.com/zhoux77899/router-forge/internal/store"
)

type fakeExecutor struct {
	gatewayURL    string
	block         bool
	hostAbsent    bool
	omitAgent     bool
	harborVersion string
	taskCommit    string
	entered       chan struct{}
	calls         atomic.Int64
	resolveCalls  atomic.Int64
}

func (f *fakeExecutor) Inspect(ctx context.Context, e domain.Environment) (domain.Inspection, error) {
	version := f.harborVersion
	if version == "" {
		version = "9.8.7"
	}
	return domain.Inspection{EnvironmentID: e.ID, OS: "linux", Arch: "amd64", HomeDir: "/home/test", WorkDir: "/home/test/jobs", Ready: true, Tools: []domain.ToolStatus{{Name: "uv", Path: "/usr/bin/uv", Available: true, Version: "8.7.6", VersionOutput: "uv 8.7.6"}, {Name: "harbor", Path: "/usr/bin/harbor", Available: true, Version: version, VersionOutput: "harbor " + version}}, Agents: []domain.Agent{{Name: "opencode", Command: "opencode", Installed: !f.hostAbsent, HarborSupported: true, Version: "host-only-hint", SupportedProtocols: []domain.Protocol{domain.ChatCompletions}, ContainerInstall: "native Harbor setup"}}, Issues: []string{}}, nil
}
func (f *fakeExecutor) Resolve(_ context.Context, _ domain.Environment, suite domain.Suite) (domain.ResolvedBenchmark, error) {
	f.resolveCalls.Add(1)
	commit := f.taskCommit
	if commit == "" {
		commit = strings.Repeat("a", 40)
	}
	suite.Version, suite.RegistryURL, suite.TaskCount = "current-fixture-version", "https://registry.fixture.test/catalog.json", 1
	task := domain.BenchmarkTask{Name: "django__django-13741", Kind: "git", Path: "tasks/django__django-13741", GitURL: "https://code.fixture.test/tasks.git", GitCommit: commit}
	resolved := domain.ResolvedBenchmark{Suite: suite, RegistryKind: "json", ResolvedAt: domain.Now(), Tasks: []domain.BenchmarkTask{task}}
	resolved.ResolutionHash, _ = resolved.Hash()
	return resolved, nil
}
func (f *fakeExecutor) Tasks(ctx context.Context, env domain.Environment, suite domain.Suite) ([]domain.BenchmarkTask, error) {
	resolved, err := f.Resolve(ctx, env, suite)
	return resolved.Tasks, err
}
func (f *fakeExecutor) Execute(ctx context.Context, e domain.Environment, spec domain.ExecutionSpec, emit func(domain.WorkerEvent), observe func(domain.ExecutionObservation) error) (domain.ExecutionResult, error) {
	f.calls.Add(1)
	inspection, _ := f.Inspect(ctx, e)
	observations := []domain.ExecutionObservation{
		{Kind: "tools", Timestamp: domain.Now(), Tools: inspection.Tools, WorkDir: inspection.WorkDir},
		{Kind: "task", Timestamp: domain.Now(), Task: &spec.Snapshot.ResolvedTasks[0], RegistryURL: spec.Snapshot.Suite.RegistryURL, TaskPath: "/home/test/jobs/task", TaskName: spec.Snapshot.Spec.TaskNames[0], TrialPrefix: spec.Snapshot.Spec.TaskNames[0]},
		{Kind: "agent", Timestamp: domain.Now(), AgentVersion: "2.4.0", AgentVersionOutput: "opencode 2.4.0"},
	}
	for _, observation := range observations {
		if observation.Kind == "agent" && f.omitAgent {
			continue
		}
		if err := observe(observation); err != nil {
			return domain.ExecutionResult{State: "failed", ExitCode: -1, ExceptionType: "ObservationError"}, err
		}
	}
	if f.entered != nil {
		close(f.entered)
	}
	if f.block {
		<-ctx.Done()
		return domain.ExecutionResult{State: "cancelled", ExitCode: -1}, ctx.Err()
	}
	body := `{"model":"mock-model","messages":[{"role":"user","content":"unit-only fake agent"}]}`
	req, err := http.NewRequestWithContext(ctx, "POST", f.gatewayURL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+spec.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	emit(domain.WorkerEvent{Kind: "verifier", Message: "Independent fixture verifier completed", Timestamp: domain.Now()})
	reward := 0.0
	return domain.ExecutionResult{State: "completed", TrialName: "fixture__trial", Reward: &reward, ExceptionType: "AgentTimeoutError", ExitCode: 0, ResultPath: "/home/test/jobs/fixture/result.json", AgentVersion: "2.4.0", ImageDigests: []string{"sha256:fixture"}, HarborMetrics: json.RawMessage(`{"n_input_tokens":1000,"prompt":"must not be persisted"}`)}, nil
}
func (f *fakeExecutor) Close() error { return nil }

type fixture struct {
	store    *store.Store
	app      *Server
	http     *httptest.Server
	exec     *fakeExecutor
	token    string
	provider *httptest.Server
	calls    atomic.Int64
}

func setup(t *testing.T, block bool) *fixture {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: s, token: domain.NewID("control"), exec: &fakeExecutor{block: block, entered: make(chan struct{})}}
	f.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer unit-only-provider-key" {
			t.Error("upstream auth replacement failed")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"chatcmpl-fixture","model":"mock-model","choices":[{"message":{"role":"assistant","content":"fixture"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
	}))
	gw := gateway.New(s)
	gwh := httptest.NewServer(gw)
	f.exec.gatewayURL = gwh.URL
	f.app, err = New(context.Background(), s, gw, f.exec, Config{ControlToken: f.token})
	if err != nil {
		t.Fatal(err)
	}
	f.http = httptest.NewServer(f.app)
	t.Cleanup(func() { f.http.Close(); f.app.Close(); gwh.Close(); f.provider.Close(); s.Close() })
	return f
}
func (f *fixture) request(t *testing.T, method, path string, value any, admin bool) (int, []byte) {
	t.Helper()
	var body []byte
	if value != nil {
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, f.http.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if admin {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, b
}
func result[T any](t *testing.T, status int, body []byte) T {
	t.Helper()
	var value T
	if status != 200 {
		t.Fatalf("HTTP %d: %s", status, body)
	}
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func (f *fixture) draft(t *testing.T) domain.Run {
	t.Helper()
	code, body := f.request(t, "POST", "/api/environments", domain.Environment{Name: "Mock local", Type: "local"}, true)
	env := result[domain.Environment](t, code, body)
	key := "unit-only-provider-key"
	code, body = f.request(t, "POST", "/api/providers", domain.ProviderInput{Name: "Mock", BaseURL: f.provider.URL + "/v1", APIKey: &key}, true)
	provider := result[domain.Provider](t, code, body)
	if bytes.Contains(body, []byte(key)) {
		t.Fatal("key leaked")
	}
	code, body = f.request(t, "POST", "/api/models", domain.Model{ProviderID: provider.ID, Name: "mock-model", Protocol: domain.ChatCompletions}, true)
	model := result[domain.Model](t, code, body)
	input := domain.RunSpec{EnvironmentID: env.ID, ModelID: model.ID, SuiteID: "swebench-verified", TaskNames: []string{"django__django-13741"}, Agent: "opencode", TimeoutSec: 60, SetupTimeoutSec: 60, MaxRequests: 4, MaxOutputTokens: 128, Concurrency: 1, Attempts: 1}
	code, body = f.request(t, "POST", "/api/runs", input, true)
	return result[domain.Run](t, code, body)
}
func waitRun(t *testing.T, f *fixture, id string) domain.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := f.store.Run(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "failed" || run.State == "completed" || run.State == "cancelled" || run.State == "interrupted" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not reach a terminal state")
	return domain.Run{}
}

func TestDraftApprovalMeteringAndIndependentScore(t *testing.T) {
	f := setup(t, false)
	draft := f.draft(t)
	if draft.State != "draft" || f.calls.Load() != 0 || f.exec.calls.Load() != 0 {
		t.Fatal("creating draft launched inference")
	}
	code, _ := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": false}, true)
	if code != 400 || f.calls.Load() != 0 {
		t.Fatal("unapproved inference was accepted")
	}
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	result[domain.Run](t, code, body)
	run := waitRun(t, f, draft.ID)
	if run.State != "failed" || f.calls.Load() != 1 || run.Tasks[0].Attempts[0].Reward == nil || *run.Tasks[0].Attempts[0].Reward != 0 || run.Tasks[0].Attempts[0].ExceptionType != "AgentTimeoutError" {
		t.Fatalf("%+v", run)
	}
	if run.Summary.RequestCount != 1 || run.Summary.InputTokens != 7 || run.Summary.OutputTokens != 3 || run.Summary.UnknownCostCount != 1 {
		t.Fatalf("%+v", run.Summary)
	}
	if bytes.Contains(run.Tasks[0].Attempts[0].HarborMetrics, []byte("prompt")) {
		t.Fatal("raw Harbor payload persisted")
	}
	code, body = f.request(t, "GET", "/api/runs/"+run.ID, nil, false)
	public := result[domain.Run](t, code, body)
	if public.ID != run.ID || bytes.Contains(body, []byte("unit-only-provider-key")) || bytes.Contains(body, []byte("gateway_token")) {
		t.Fatal("read-only details leaked secrets")
	}
	code, body = f.request(t, "GET", "/api/requests?run_id="+run.ID, nil, false)
	page := result[domain.Page[domain.RequestRecord]](t, code, body)
	if len(page.Items) != 1 || page.Items[0].AttemptID != run.Tasks[0].Attempts[0].ID {
		t.Fatal("request attribution lost")
	}
	code, _ = f.request(t, "POST", "/api/runs/"+run.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	if code != 409 || f.exec.calls.Load() != 1 {
		t.Fatal("terminal run was automatically retried")
	}
}

func TestReadOnlyCannotManageOrRebind(t *testing.T) {
	f := setup(t, false)
	for _, path := range []string{"/api/providers", "/api/environments", "/api/models"} {
		code, _ := f.request(t, "GET", path, nil, false)
		if code != 401 {
			t.Fatal(path, code)
		}
	}
	for _, path := range []string{"/api/providers", "/api/environments", "/api/models", "/api/runs"} {
		code, _ := f.request(t, "POST", path, map[string]any{}, false)
		if code != 401 {
			t.Fatal(path, code)
		}
	}
	code, _ := f.request(t, "GET", "/api/providers?token="+f.token, nil, false)
	if code != 401 {
		t.Fatal("query token authorized management")
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Host = "attacker.test" }, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.test") }} {
		req, _ := http.NewRequest("GET", f.http.URL+"/api/runs", nil)
		mutate(req)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatal("accepted untrusted origin/host")
		}
	}
}

func TestCancellationOnlyAffectsOwnedRun(t *testing.T) {
	f := setup(t, true)
	draft := f.draft(t)
	code, body := f.request(t, "POST", "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, true)
	result[domain.Run](t, code, body)
	select {
	case <-f.exec.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	code, _ = f.request(t, "POST", "/api/runs/unowned/cancel", map[string]any{}, true)
	if code != 404 {
		t.Fatal("unknown cancel accepted")
	}
	code, body = f.request(t, "POST", "/api/runs/"+draft.ID+"/cancel", map[string]any{}, true)
	result[domain.Run](t, code, body)
	run := waitRun(t, f, draft.ID)
	if run.State != "cancelled" || f.calls.Load() != 0 {
		t.Fatalf("%+v", run)
	}
}

func TestAgentOverridesCannotBypassGateway(t *testing.T) {
	for _, key := range []string{"api_key", "api_base", "model_name", "version", "env", "config_path"} {
		if validateAgentKwargs(map[string]json.RawMessage{key: json.RawMessage(`"override"`)}) == nil {
			t.Fatal(key)
		}
	}
	if validateAgentKwargs(map[string]json.RawMessage{"temperature": json.RawMessage(`0.2`)}) != nil {
		t.Fatal("valid scalar option rejected")
	}
}

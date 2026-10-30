package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func mockIntegrationSpec(e domain.Environment) domain.ExecutionSpec {
	provider := domain.Provider{ID: "fixture_provider", BaseURL: "http://127.0.0.1:1"}
	model := domain.Model{ID: "fixture_model", ProviderID: provider.ID, Name: mockIntegrationModel, Protocol: domain.ChatCompletions, Pricing: &domain.Pricing{Currency: "USD", InputPerMillion: "0", OutputPerMillion: "0", Source: "offline fixture, not a real tariff"}}
	return domain.ExecutionSpec{RunID: domain.NewID("fixture_run"), TaskID: domain.NewID("fixture_task"), AttemptID: domain.NewID("fixture_attempt"), JobName: "fixture_offline_transport", GatewayToken: "fixture-lease", Snapshot: domain.RunSnapshot{Spec: domain.RunSpec{TaskNames: []string{"fixture_transport"}, ConfirmPaid: false}, Environment: e, Model: model, Provider: provider}}
}

func TestOfflineVerificationCannotActivatePaidOrUnattributedWork(t *testing.T) {
	e := domain.Environment{ID: "fixture-env", Type: "wsl", Distribution: "Ubuntu"}
	image := "sha256:" + strings.Repeat("1", 64)
	spec := mockIntegrationSpec(e)
	if err := validateMockIntegration(e, spec, image); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.ExecutionSpec){func(s *domain.ExecutionSpec) { s.Snapshot.Spec.ConfirmPaid = true }, func(s *domain.ExecutionSpec) { s.Snapshot.Model.Name = "real-model" }, func(s *domain.ExecutionSpec) { s.Snapshot.Provider.BaseURL = "https://provider.example" }, func(s *domain.ExecutionSpec) { s.RunID = "real_run" }, func(s *domain.ExecutionSpec) { s.Snapshot.Environment.Distribution = "Other" }} {
		s := spec
		mutate(&s)
		if validateMockIntegration(e, s, image) == nil {
			t.Fatal("unsafe/off-scope offline verification accepted")
		}
	}
	if validateMockIntegration(e, spec, "alpine:latest") == nil {
		t.Fatal("mutable or absent fixture image accepted")
	}
}

type fixtureLedger struct {
	mu      sync.Mutex
	records map[string]domain.RequestRecord
}

func (l *fixtureLedger) BeginRequest(ctx context.Context, r domain.RequestRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records[r.ID] = r
	return nil
}
func (l *fixtureLedger) EndRequest(ctx context.Context, r domain.RequestRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records[r.ID] = r
	return nil
}
func (l *fixtureLedger) snapshot() []domain.RequestRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []domain.RequestRecord{}
	for _, r := range l.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func mockNativeRequest(which string, stream bool) []byte {
	options := ""
	if stream {
		options = `,"stream_options":{"include_usage":true}`
	}
	return []byte(fmt.Sprintf(" \n{\"model\":\"router-forge-fixture\",\"messages\":[{\"role\":\"user\",\"content\":\"FIXTURE %s λ — not a benchmark prompt\"}],\"max_tokens\":32,\"stream\":%t%s}\n", which, stream, options))
}

const mockNativeJSON = ` {"id":"fixture-json","object":"chat.completion","model":"router-forge-fixture","choices":[{"message":{"role":"assistant","content":"FIXTURE λ output, not an answer to a benchmark"}}],"usage":{"prompt_tokens":7,"completion_tokens":11,"total_tokens":18}} `
const mockNativeSSEFirst = "data: {\"id\":\"fixture-sse\",\"model\":\"router-forge-fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"FIXTURE λ\"}}]}\n\n"
const mockNativeSSELast = "data: {\"id\":\"fixture-sse\",\"model\":\"router-forge-fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":11,\"total_tokens\":18}}\n\ndata: [DONE]\n\n"

// Explicitly opt-in: existing Docker image, synthetic keys, no Harbor or agent run.
func TestWSLDockerMockIntegration(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("ROUTER_FORGE_VERIFY_MOCK_INTEGRATION") != "1" {
		t.Skip("opt-in actual WSL Docker relay/HTTP2/mock provider verification")
	}
	image := os.Getenv("ROUTER_FORGE_MOCK_IMAGE_ID")
	if !imageIDPattern.MatchString(image) {
		t.Fatal("ROUTER_FORGE_MOCK_IMAGE_ID must be an already-present immutable sha256 image ID with python3")
	}
	distribution := os.Getenv("ROUTER_FORGE_MOCK_DISTRIBUTION")
	if distribution == "" {
		distribution = "Ubuntu"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	var observationsMu sync.Mutex
	observations := map[string]string{}
	nativeHTTP2 := true
	nativeLeaseAuth := true
	providerFreshAuth := true
	aborted := make(chan struct{})
	sseObserved := make(chan struct{})
	releaseSSE := make(chan struct{})
	releaseOnce := sync.Once{}
	defer releaseOnce.Do(func() { close(releaseSSE) })
	upstreamKey := "fixture-upstream-key-not-a-real-credential"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		which := ""
		for _, candidate := range []string{"json", "sse", "abort"} {
			if bytes.Equal(body, mockNativeRequest(candidate, candidate != "json")) {
				which = candidate
				break
			}
		}
		observationsMu.Lock()
		providerFreshAuth = providerFreshAuth && r.Header.Get("Authorization") == "Bearer "+upstreamKey && r.Header.Get("x-api-key") == ""
		if which != "" {
			sum := sha256.Sum256(body)
			observations[which] = hex.EncodeToString(sum[:])
		}
		observationsMu.Unlock()
		if which == "" || r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "invalid synthetic native request", 400)
			return
		}
		w.Header().Set("X-Request-Id", "fixture-provider-"+which)
		if which == "json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, mockNativeJSON)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, mockNativeSSEFirst)
		w.(http.Flusher).Flush()
		if which == "abort" {
			<-r.Context().Done()
			close(aborted)
			return
		}
		close(sseObserved)
		select {
		case <-releaseSSE:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, mockNativeSSELast)
	}))
	defer provider.Close()
	defer provider.CloseClientConnections()
	env := domain.Environment{ID: domain.NewID("fixture_env"), Type: "wsl", Distribution: distribution, WorkerOS: "linux", WorkerArch: "amd64"}
	spec := mockIntegrationSpec(env)
	spec.Snapshot.Provider.BaseURL = provider.URL + "/v1"
	ledger := &fixtureLedger{records: map[string]domain.RequestRecord{}}
	modelGateway := gateway.New(ledger)
	token, err := modelGateway.Register(gateway.Lease{RunID: spec.RunID, TaskID: spec.TaskID, AttemptID: spec.AttemptID, Provider: spec.Snapshot.Provider, Model: spec.Snapshot.Model, APIKey: upstreamKey, MaxRequests: 3, MaxOutputTokens: 64, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer modelGateway.Revoke(token)
	spec.GatewayToken = token
	injected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observationsMu.Lock()
		nativeHTTP2 = nativeHTTP2 && r.ProtoMajor == 2
		nativeLeaseAuth = nativeLeaseAuth && r.Header.Get("Authorization") == "Bearer "+token
		observationsMu.Unlock()
		modelGateway.ServeHTTP(w, r)
	})
	root := projectTestDir(t)
	buildWork := filepath.Join(root, "build-work")
	if err := os.Mkdir(buildWork, 0700); err != nil {
		t.Fatal(err)
	}
	workerName, _ := WorkerFilename("linux", "amd64")
	repository, _ := filepath.Abs(filepath.Join("..", ".."))
	build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(root, workerName), "./apps/worker")
	build.Dir = repository
	build.Env = append(transportEnvironment(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOTMPDIR="+buildWork)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cross-building current production worker: %v\n%s", err, output)
	}
	manager := New(root, injected)
	defer manager.Close()
	replyReady := make(chan commandReply, 1)
	requestErrors := make(chan error, 1)
	eventReady := make(chan struct{}, 1)
	go func() {
		reply, err := manager.request(ctx, env, workerCommand{Kind: "verify-mock", Environment: env, Spec: &spec, MockImageID: image}, func(event domain.WorkerEvent) {
			if event.Kind == "fixture_relay_ready" {
				select {
				case eventReady <- struct{}{}:
				default:
				}
			}
		})
		replyReady <- reply
		requestErrors <- err
	}()
	select {
	case <-sseObserved:
	case earlyErr := <-requestErrors:
		earlyReply := <-replyReady
		if r := earlyReply.MockResult; r != nil {
			t.Logf("fixture stage status json=%d sse=%d abort=%d management=%d wrong_auth=%d cleaned=%t", r.JSONStatus, r.SSEStatus, r.AbortStatus, r.ManagementStatus, r.WrongAuthStatus, r.CleanupCompleted)
		}
		t.Fatalf("offline request ended before native SSE was observed: %v (worker: %s)", earlyErr, earlyReply.Error)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-eventReady:
	case <-time.After(5 * time.Second):
		t.Fatal("stdio control event was not delivered while model SSE was active")
	}
	releaseOnce.Do(func() { close(releaseSSE) })
	var reply commandReply
	select {
	case reply = <-replyReady:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-requestErrors; err != nil {
		t.Fatalf("offline verification: %v (%s)", err, reply.Error)
	}
	if reply.MockResult == nil {
		t.Fatal("worker did not return explicit fixture records")
	}
	result := reply.MockResult
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("container stream abort did not cancel the authenticated stdio/provider request")
	}
	for _, pair := range []struct{ actual, want string }{{result.JSONBody, mockNativeJSON}, {result.SSEBody, mockNativeSSEFirst + mockNativeSSELast}, {result.AbortPrefix, mockNativeSSEFirst}} {
		got, err := base64.StdEncoding.DecodeString(pair.actual)
		if err != nil || string(got) != pair.want {
			t.Fatal("native JSON/SSE response bytes changed across the Docker relay/HTTP2 gateway")
		}
	}
	if !result.Fixture || result.BenchmarkExecuted || result.ModelInference || !result.CleanupCompleted || !dockerID.MatchString(result.ContainerID) || result.ImageID != image {
		t.Fatal("offline attribution, image identity or owned cleanup failed")
	}
	observationsMu.Lock()
	authOK := nativeHTTP2 && nativeLeaseAuth && providerFreshAuth
	observed := map[string]string{}
	for k, v := range observations {
		observed[k] = v
	}
	observationsMu.Unlock()
	if !authOK || len(observed) != 3 {
		t.Fatal("HTTP2, lease authentication, fresh mock-provider authentication or native request bytes were not preserved")
	}
	var records []domain.RequestRecord
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		records = ledger.snapshot()
		finished := 0
		for _, r := range records {
			if r.FinishedAt != nil {
				finished++
			}
		}
		if finished == 3 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(records) != 3 {
		t.Fatal("fixture request ledger did not attribute exactly three native calls")
	}
	for _, r := range records {
		if r.RunID != spec.RunID || r.TaskID != spec.TaskID || r.AttemptID != spec.AttemptID || r.ModelID != spec.Snapshot.Model.ID || r.ProviderID != spec.Snapshot.Provider.ID || r.FinishedAt == nil {
			t.Fatal("native fixture request attribution/finalization changed")
		}
		if r.CostAmount != nil && *r.CostAmount != "0" {
			t.Fatal("synthetic fixture unexpectedly records non-zero cost")
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	check := exec.CommandContext(ctx, "wsl.exe", "--distribution", distribution, "--exec", "docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "label=io.router-forge.owner="+result.Owner)
	if output, err := check.Output(); err != nil || strings.TrimSpace(string(output)) != "" {
		t.Fatal("owned offline fixture container remains after cleanup")
	}
	proof := struct {
		Fixture           bool                   `json:"fixture"`
		BenchmarkScores   any                    `json:"benchmark_scores"`
		Source            string                 `json:"source"`
		ProductionWorker  bool                   `json:"production_worker"`
		HTTP2             bool                   `json:"authenticated_stdio_http2"`
		NativeRequests    map[string]string      `json:"native_request_sha256"`
		NativeJSON        bool                   `json:"native_json_bytes_preserved"`
		NativeSSE         bool                   `json:"native_sse_bytes_preserved"`
		ProviderAbort     bool                   `json:"provider_abort_observed"`
		FreshProviderAuth bool                   `json:"fresh_synthetic_provider_auth"`
		Result            *mockIntegrationResult `json:"worker"`
		Records           []domain.RequestRecord `json:"synthetic_requests"`
	}{true, nil, "actual WSL Docker -> private bridge relay -> authenticated stdio HTTP2 -> injected gateway -> local synthetic provider; not a benchmark or live inference", true, true, observed, true, true, true, true, result, records}
	data, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join("..", "..", "benchmarks", "harbor", "fixtures", "mock-integration-observed.json")
	if err := os.WriteFile(artifact, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture-only proof: production worker=%s image=%s container=%s relay=%s records=%d native JSON/SSE unchanged, HTTP2/lease/fresh mock auth verified, upstream abort observed, owned cleanup complete; benchmark_scores=null", result.WorkerVersion, result.ImageID, result.ContainerID, result.RelayURL, len(records))
	for _, r := range records {
		cost := "unknown"
		if r.CostAmount != nil {
			cost = *r.CostAmount
		}
		t.Logf("synthetic record %s run=%s task=%s attempt=%s state=%s usage=%s cost=%s", r.ID, r.RunID, r.TaskID, r.AttemptID, r.State, r.Usage.Completeness, cost)
	}
}

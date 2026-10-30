package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	backend "github.com/zhoux77899/router-forge/internal/runtime"
)

// Only the external model service is synthetic; Harbor retains the original SWE task and verifier.
func TestWSLHarborMockTrial(t *testing.T) {
	if os.Getenv("ROUTER_FORGE_VERIFY_HARBOR_MOCK") != "1" {
		t.Skip("set ROUTER_FORGE_VERIFY_HARBOR_MOCK=1 and ROUTER_FORGE_NATIVE_BUNDLE_DIR for the opt-in real Harbor fixture")
	}
	if runtime.GOOS != "windows" {
		t.Skip("initial real Harbor fixture uses Windows Desktop and WSL")
	}
	resources := os.Getenv("ROUTER_FORGE_NATIVE_BUNDLE_DIR")
	if !filepath.IsAbs(resources) {
		t.Fatal("ROUTER_FORGE_NATIVE_BUNDLE_DIR must be an absolute built bundle")
	}
	distribution := os.Getenv("ROUTER_FORGE_HARBOR_DISTRIBUTION")
	if distribution == "" {
		distribution = "Ubuntu"
	}
	requestedTask := os.Getenv("ROUTER_FORGE_HARBOR_TASK")
	if requestedTask == "" {
		requestedTask = "swe-bench/django__django-13741"
	}
	profile := t.TempDir()
	options := resourceOptions(resources, profile)
	registerBundleBackendCleanup(t, profile)
	console := newConsole()
	console.options = func() (backend.Options, error) { return options, nil }
	t.Cleanup(console.client.CloseIdleConnections)
	connection, err := console.Connect()
	if err != nil {
		t.Fatal(err)
	}
	workDir := "/var/tmp/router-forge-harbor-" + domain.NewID("fixture")
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Private failed-trial artifacts retained for diagnosis: %s (%s)", workDir, distribution)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "wsl.exe", "-d", distribution, "--exec", "rm", "-rf", "--", workDir).Run(); err != nil {
			t.Errorf("remove owned fixture work directory: %v", err)
		}
	})

	const key = "fixture-only-harbor-api-key"
	const model = "fixture-model"
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("fixture provider received an unexpected endpoint or credential")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&request) != nil || request.Model != model {
			t.Error("fixture model identity was not preserved")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := fmt.Sprintf("chatcmpl_fixture_%d", providerCalls.Add(1))
		w.Header().Set("X-Request-ID", id)
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunks := []map[string]any{
				{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"role": "assistant", "content": "Offline infrastructure fixture complete. No files changed."}, "finish_reason": nil}}},
				{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}},
				{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 11, "total_tokens": 18}},
			}
			for _, chunk := range chunks {
				chunk["id"], chunk["model"], chunk["object"], chunk["created"] = id, model, "chat.completion.chunk", time.Now().Unix()
				encoded, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", encoded)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "model": model, "object": "chat.completion", "created": time.Now().Unix(),
			"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "Offline infrastructure fixture complete. No files changed."}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 7, "completion_tokens": 11, "total_tokens": 18},
		})
	}))
	t.Cleanup(provider.Close)

	var environment domain.Environment
	bundleAPICall(t, console, http.MethodPost, "/api/environments", domain.Environment{
		Name: "Synthetic Harbor acceptance", Type: "wsl", Distribution: distribution, WorkDir: workDir,
		CACertPath: os.Getenv("ROUTER_FORGE_HARBOR_CA"), ProxyURL: os.Getenv("ROUTER_FORGE_HARBOR_PROXY"), NoProxy: os.Getenv("ROUTER_FORGE_HARBOR_NO_PROXY"),
	}, &environment)
	var providerConfig domain.Provider
	bundleAPICall(t, console, http.MethodPost, "/api/providers", domain.ProviderInput{
		Name: "Synthetic local provider; no inference", BaseURL: provider.URL + "/v1", APIKey: stringPointer(key),
	}, &providerConfig)
	var modelConfig domain.Model
	bundleAPICall(t, console, http.MethodPost, "/api/models", domain.Model{
		ProviderID: providerConfig.ID, Name: model, DisplayName: "Synthetic infrastructure fixture", Protocol: domain.ChatCompletions,
		Pricing: &domain.Pricing{Currency: "USD", InputPerMillion: "0", OutputPerMillion: "0", Source: "fixture-only synthetic accounting; not a real model price"},
	}, &modelConfig)

	var draft domain.Run
	bundleAPICall(t, console, http.MethodPost, "/api/runs", domain.RunSpec{
		EnvironmentID: environment.ID, ModelID: modelConfig.ID, SuiteID: "swebench-verified", TaskNames: []string{requestedTask},
		Agent: "opencode", AgentKwargs: map[string]json.RawMessage{"disable_web_search": json.RawMessage("true")},
		TimeoutSec: 90, SetupTimeoutSec: 900, MaxRequests: 5, MaxOutputTokens: 256, Concurrency: 1, Attempts: 1,
	}, &draft)
	if draft.State != "draft" || draft.Snapshot.Spec.ConfirmPaid || providerCalls.Load() != 0 {
		t.Fatal("draft preparation did not preserve the no-inference boundary")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = console.Request(ctx, http.MethodPost, "/api/runs/"+draft.ID+"/cancel", "{}")
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			response, err := console.Request(ctx, http.MethodGet, "/api/runs/"+draft.ID, "")
			var run domain.Run
			if err != nil || json.Unmarshal([]byte(response.Body), &run) != nil || harborFixtureTerminal(run.State) {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Error("owned Harbor fixture did not finish cancellation")
	})
	var started domain.Run
	bundleAPICall(t, console, http.MethodPost, "/api/runs/"+draft.ID+"/start", map[string]bool{"confirm_paid": true}, &started)
	deadline := time.Now().Add(40 * time.Minute)
	run := started
	lastState := ""
	var after int64
	for !harborFixtureTerminal(run.State) {
		if time.Now().After(deadline) {
			t.Fatal("bounded real Harbor fixture timed out")
		}
		if run.State != lastState {
			t.Logf("fixture run %s: %s", run.ID, run.State)
			lastState = run.State
		}
		var events []domain.JobEvent
		bundleAPICall(t, console, http.MethodGet, fmt.Sprintf("/api/runs/%s/events?after=%d", run.ID, after), nil, &events)
		for _, event := range events {
			if event.ID > after {
				after = event.ID
			}
			t.Logf("fixture lifecycle: %s: %s", event.Kind, event.Message)
		}
		time.Sleep(time.Second)
		bundleAPICall(t, console, http.MethodGet, "/api/runs/"+draft.ID, nil, &run)
	}
	var records domain.Page[domain.RequestRecord]
	bundleAPICall(t, console, http.MethodGet, "/api/requests?run_id="+run.ID+"&limit=100", nil, &records)
	privateProvenanceVerified := false
	saveEvidence := func(verified bool) {
		evidencePath := os.Getenv("ROUTER_FORGE_HARBOR_MOCK_EVIDENCE")
		if evidencePath == "" {
			return
		}
		if !filepath.IsAbs(evidencePath) {
			t.Fatal("fixture evidence path must be absolute")
		}
		data, err := json.MarshalIndent(map[string]any{
			"fixture_only": true, "model_inference": false, "benchmark_scores": nil,
			"acceptance_verified": verified, "private_provenance_verified": privateProvenanceVerified,
			"requested_original_swe_task_and_verifier": true, "synthetic_provider_calls": providerCalls.Load(), "run": run, "requests": records,
		}, "", "  ")
		if err != nil {
			t.Fatal("fixture evidence could not be encoded")
		}
		if strings.Contains(string(data), key) || strings.Contains(string(data), console.info.ControlToken) {
			t.Fatal("fixture evidence included a private credential")
		}
		if err := os.WriteFile(evidencePath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saveEvidence(false)
	if run.State != "completed" || len(run.Tasks) != 1 || len(run.Tasks[0].Attempts) != 1 {
		t.Fatalf("native Harbor fixture did not complete with an independent verifier: state=%s error=%s", run.State, run.Error)
	}
	attempt := run.Tasks[0].Attempts[0]
	if attempt.Reward == nil || *attempt.Reward != 0 || attempt.ExceptionType != "" || attempt.JobName == "" || attempt.TrialName == "" || attempt.AgentVersion == "" || run.Snapshot.Runtime == nil || run.Snapshot.Runtime.AgentVersion != attempt.AgentVersion || run.Snapshot.Runtime.AgentVersionOutput == "" || len(attempt.ImageDigests) == 0 {
		t.Fatal("independent original verifier outcome and actual container identities were not retained")
	}
	if records.Total == 0 || records.Total != int(providerCalls.Load()) || records.Total > 5 {
		t.Fatal("real Harbor requests were not bounded and attributed to the synthetic provider")
	}
	for _, record := range records.Items {
		if record.RunID != run.ID || record.TaskID != run.Tasks[0].ID || record.AttemptID != attempt.ID || record.ProviderID != providerConfig.ID || record.ModelID != modelConfig.ID || record.State != "completed" || record.Usage.InputTokens == nil || record.Usage.OutputTokens == nil || *record.Usage.InputTokens != 7 || *record.Usage.OutputTokens != 11 {
			t.Fatal("a real agent request lost its approved attribution or synthetic native usage")
		}
	}
	var provenance struct {
		ConfigHash        string                  `json:"config_hash"`
		DatasetVersion    string                  `json:"dataset_version"`
		DatasetDigest     string                  `json:"dataset_digest"`
		DatasetRevision   string                  `json:"dataset_revision"`
		Task              domain.BenchmarkTask    `json:"task"`
		Runtime           *domain.RuntimeSnapshot `json:"runtime"`
		AgentInstallation string                  `json:"agent_installation"`
		AgentVersion      string                  `json:"agent_version"`
		ImageDigests      []string                `json:"image_digests"`
		ExecutionState    string                  `json:"execution_state"`
	}
	provenanceCtx, stopProvenance := context.WithTimeout(context.Background(), 15*time.Second)
	data, provenanceErr := exec.CommandContext(provenanceCtx, "wsl.exe", "-d", distribution, "--exec", "cat", "--", workDir+"/"+run.ID+"/"+attempt.ID+"/provenance.json").Output()
	stopProvenance()
	if provenanceErr != nil || len(data) > 1<<20 || json.Unmarshal(data, &provenance) != nil {
		t.Fatal("final private native provenance could not be read")
	}
	privateRuntime, _ := json.Marshal(provenance.Runtime)
	acknowledgedRuntime, _ := json.Marshal(run.Snapshot.Runtime)
	if provenance.ConfigHash != draft.Snapshot.ConfigHash || provenance.DatasetVersion != draft.Snapshot.Suite.Version || provenance.DatasetDigest != draft.Snapshot.DatasetDigest || provenance.DatasetRevision != draft.Snapshot.DatasetRevision || provenance.Task != draft.Snapshot.ResolvedTasks[0] || string(privateRuntime) != string(acknowledgedRuntime) || provenance.AgentInstallation != "harbor_default" || provenance.AgentVersion != attempt.AgentVersion || strings.Join(provenance.ImageDigests, "\n") != strings.Join(attempt.ImageDigests, "\n") || provenance.ExecutionState != "completed" {
		t.Fatal("final private evidence lost the approved source, actual acknowledged runtime or original image identities")
	}
	privateProvenanceVerified = true
	reattached := newConsole()
	reattached.options = func() (backend.Options, error) { return options, nil }
	t.Cleanup(reattached.client.CloseIdleConnections)
	reopened, err := reattached.Connect()
	if err != nil || reopened.PID != connection.PID {
		t.Fatal("completed Harbor result could not be reattached through the same backend")
	}
	var persisted domain.Run
	bundleAPICall(t, reattached, http.MethodGet, "/api/runs/"+run.ID, nil, &persisted)
	if persisted.State != "completed" || persisted.Summary.RequestCount != records.Total || persisted.Snapshot.Runtime == nil || persisted.Snapshot.Runtime.AgentVersion != attempt.AgentVersion || persisted.Snapshot.Runtime.AgentVersionOutput != run.Snapshot.Runtime.AgentVersionOutput || persisted.Snapshot.ConfigHash != draft.Snapshot.ConfigHash || persisted.Snapshot.DatasetDigest != draft.Snapshot.DatasetDigest {
		t.Fatal("completed fixture result, acknowledged runtime identity, approved source or accounting did not persist")
	}
	saveEvidence(true)
	t.Logf("Original one-task Harbor trial verified with synthetic provider only: run=%s task=%s attempt=%s requests=%d reward=0; benchmark_scores=null", run.ID, run.Tasks[0].ID, attempt.ID, records.Total)
}

func harborFixtureTerminal(state string) bool {
	switch state {
	case "completed", "failed", "cancelled", "interrupted":
		return true
	default:
		return false
	}
}

func stringPointer(value string) *string { return &value }

package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExitZeroAndRewardZeroDoNotTurnAgentTimeoutIntoSolved(t *testing.T) {
	result, err := ParseHarborResult(fixture(t, "result-timeout.json"), "/fixture/job/trial/result.json", 0)
	if err != nil || result.State != "timed_out" || result.Reward == nil || *result.Reward != 0 || result.ExceptionType != "AgentTimeoutError" || result.ExitCode != 0 {
		t.Fatalf("result: %#v %v", result, err)
	}
	if result.AgentVersion != "" {
		t.Fatal("Harbor requested-version metadata was mislabeled as observed CLI version")
	}
	if strings.Contains(string(result.HarborMetrics), "must never") || strings.Contains(result.Error, "secret") {
		t.Fatal("raw task diagnostics leaked")
	}
}
func TestCLIExitRemainsSeparateFromVerifierReward(t *testing.T) {
	data := []byte(`{"task_name":"fixture__task-1","trial_name":"fixture__task-1__ABC","finished_at":"2026-01-01T00:00:01Z","verifier_result":{"rewards":{"reward":1}}}`)
	result, err := ParseHarborResult(data, "/fixture/result.json", 2)
	if err != nil || result.State != "cli_error" || result.Reward == nil || *result.Reward != 1 || result.ExitCode != 2 {
		t.Fatalf("CLI status/reward: %#v %v", result, err)
	}
}
func TestPartialAndNonAtomicResultWritesAreRetried(t *testing.T) {
	dir := projectTestDir(t)
	trial := filepath.Join(dir, "fixture__task-1__ABC123")
	if err := os.Mkdir(trial, 0700); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(trial, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"task_name":`), 0600); err != nil {
		t.Fatal(err)
	}
	data := fixture(t, "result-timeout.json")
	go func() { time.Sleep(100 * time.Millisecond); _ = os.WriteFile(resultPath, data, 0600) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := ReadHarborResult(ctx, dir, "fixture__task-1", 0)
	if err != nil || result.ResultPath != resultPath || result.State != "timed_out" {
		t.Fatalf("non-atomic result: %#v %v", result, err)
	}
	if _, err = ParseHarborResult([]byte(`{"task_name":"fixture__task-1","finished_at":null}`), resultPath, 0); err == nil {
		t.Fatal("partial result was marked complete")
	}
}

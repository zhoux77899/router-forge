package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOneExplicitTaskAndBoundedRun(t *testing.T) {
	valid := RunSpec{EnvironmentID: "env", ModelID: "model", SuiteID: "swebench-verified", TaskNames: []string{"swe-bench/django__django-13741"}, Agent: "opencode", TimeoutSec: 900, SetupTimeoutSec: 1200, MaxRequests: 32, MaxOutputTokens: 4096, Concurrency: 1, Attempts: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RunSpec){func(s *RunSpec) { s.TaskNames = append(s.TaskNames, "another") }, func(s *RunSpec) { s.Concurrency = 2 }, func(s *RunSpec) { s.Attempts = 2 }, func(s *RunSpec) { s.AgentVersion = "1.19.4" }, func(s *RunSpec) { s.MaxRequests = 0 }, func(s *RunSpec) { s.TimeoutSec = 0 }} {
		copy := valid
		mutate(&copy)
		if copy.Validate() == nil {
			t.Fatal("unbounded run or legacy agent installation request was accepted")
		}
	}
}

func TestSnapshotHasNoCredentialField(t *testing.T) {
	b, err := json.Marshal(RunSnapshot{Provider: Provider{HasKey: true, KeyHint: "••••"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "api_key") || strings.Contains(string(b), "control_token") {
		t.Fatal("snapshot contains credential fields")
	}
}

func TestIDsAreDistinctAndTyped(t *testing.T) {
	a, b := NewID("run"), NewID("run")
	if a == b || !strings.HasPrefix(a, "run_") || len(a) != 36 {
		t.Fatal(a, b)
	}
}

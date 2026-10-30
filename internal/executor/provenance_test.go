package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestFinalProvenanceRetainsOnlyAcknowledgedRuntimeVersion(t *testing.T) {
	for _, observed := range []bool{false, true} {
		root := projectTestDir(t)
		path := filepath.Join(root, "provenance.json")
		snapshot := fixtureSpec().Snapshot
		snapshot.Runtime = &domain.RuntimeSnapshot{ObservedAt: domain.Now(), Tools: []domain.ToolStatus{{Name: "harbor", Available: true, Version: "9.8.7", VersionOutput: "harbor 9.8.7"}}}
		if observed {
			snapshot.Runtime.AgentVersion = "2.4.0"
			snapshot.Runtime.AgentVersionOutput = "opencode 2.4.0 (unknown git commit)"
			snapshot.Runtime.AgentObservedAt = domain.Now()
		}
		record := map[string]any{"task": snapshot.ResolvedTasks[0], "runtime": nil, "config_hash": snapshot.ConfigHash}
		initial, _ := json.Marshal(record)
		if err := writePrivate(path, initial); err != nil {
			t.Fatal(err)
		}
		if err := finalizeProvenance(path, record, snapshot, domain.ExecutionResult{State: "cancelled", AgentVersion: "untrusted self-report", ImageDigests: []string{"sha256:" + strings.Repeat("a", 64)}}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Runtime      *domain.RuntimeSnapshot `json:"runtime"`
			Task         domain.BenchmarkTask    `json:"task"`
			AgentVersion string                  `json:"agent_version"`
			ConfigHash   string                  `json:"config_hash"`
			State        string                  `json:"execution_state"`
			ImageDigests []string                `json:"image_digests"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Runtime == nil || saved.Runtime.Tools[0].Version != "9.8.7" || saved.Task != snapshot.ResolvedTasks[0] || saved.ConfigHash != snapshot.ConfigHash || saved.State != "cancelled" || len(saved.ImageDigests) != 1 || saved.AgentVersion != snapshot.Runtime.AgentVersion || saved.Runtime.AgentVersionOutput != snapshot.Runtime.AgentVersionOutput || strings.Contains(string(data), "untrusted self-report") {
			t.Fatal("final runtime evidence was lost, guessed or replaced", string(data))
		}
	}
}

func TestFinalProvenanceRecordsApprovedTLSPolicyOnFailureWithoutInventingConnections(t *testing.T) {
	for _, skip := range []bool{false, true} {
		root := projectTestDir(t)
		path := filepath.Join(root, "provenance.json")
		snapshot := fixtureSpec().Snapshot
		snapshot.Network = nil // Historical reports preserve their original TLS evidence.
		snapshot.Environment.SkipTLSVerification = skip
		snapshot.Environment.CACertPath = "/retained/ca.pem"
		record := map[string]any{"config_hash": snapshot.ConfigHash, "skip_tls_verification": skip}
		initial, _ := json.Marshal(record)
		if err := writePrivate(path, initial); err != nil {
			t.Fatal(err)
		}
		if err := finalizeProvenance(path, record, snapshot, domain.ExecutionResult{State: "infrastructure_error"}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved["skip_tls_verification"] != skip || saved["tls_verification_scope"] != "harbor_and_runtime_container_clients" || saved["execution_state"] != "infrastructure_error" || saved["agent_version"] != "" || snapshot.Environment.CACertPath != "/retained/ca.pem" {
			t.Fatal("final failure evidence omitted the TLS policy, changed CA or invented runtime facts", saved)
		}
	}
}

func TestFinalProvenanceWriteFailureIsNotIgnored(t *testing.T) {
	path := projectTestDir(t)
	if err := finalizeProvenance(path, map[string]any{}, fixtureSpec().Snapshot, domain.ExecutionResult{}); err == nil {
		t.Fatal("final persistence failure was ignored")
	}
}

func TestFinalProvenanceEncodingFailurePreservesInitialEvidence(t *testing.T) {
	root := projectTestDir(t)
	path := filepath.Join(root, "provenance.json")
	initial := []byte(`{"preflight":"retained"}`)
	if err := writePrivate(path, initial); err != nil {
		t.Fatal(err)
	}
	if err := finalizeProvenance(path, map[string]any{"invalid": func() {}}, fixtureSpec().Snapshot, domain.ExecutionResult{}); err == nil {
		t.Fatal("encoding failure was ignored")
	}
	data, err := os.ReadFile(path)
	entries, listErr := os.ReadDir(root)
	if err != nil || listErr != nil || string(data) != string(initial) || len(entries) != 1 {
		t.Fatal("initial evidence was lost or a staging file remained", err, listErr)
	}
}

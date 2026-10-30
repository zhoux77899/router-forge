package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestInstalledHarborCurrentSourceAndExactMaterialization(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("ROUTER_FORGE_VERIFY_NATIVE_SOURCE") != "1" {
		t.Skip("opt-in installed native Harbor source/download check; no Docker start or inference")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	environment := domain.Environment{
		ID: "native-source-fixture", Type: "local", CACertPath: os.Getenv("ROUTER_FORGE_HARBOR_CA"),
		ProxyURL: os.Getenv("ROUTER_FORGE_HARBOR_PROXY"), NoProxy: os.Getenv("ROUTER_FORGE_HARBOR_NO_PROXY"),
	}
	worker := newNativeWorker(nil)
	resolved, err := worker.Resolve(ctx, environment, domain.SWEBenchVerified())
	if err != nil {
		t.Fatal("installed Harbor source resolution failed without fallback:", err)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatal(err)
	}
	requested := os.Getenv("ROUTER_FORGE_HARBOR_TASK")
	if requested == "" {
		requested = "swe-bench/django__django-13741"
	}
	var selected *domain.BenchmarkTask
	for _, task := range resolved.Tasks {
		if task.Name == requested {
			selected = &task
			break
		}
	}
	if selected == nil {
		t.Fatalf("requested canonical task %q is absent; no substitution is allowed", requested)
	}
	tools := probeTool(ctx, environment, "harbor", []string{"--version"})
	if !tools.Available {
		t.Fatal("installed Harbor version probe failed")
	}
	root := t.TempDir()
	for _, name := range []string{"home", "scratch"} {
		if err := privateDir(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	materialized, err := materializeHarborTask(ctx, environment, tools.Path, *selected, filepath.Join(root, "task-cache"), nativeProcessEnv(environment, root, ""))
	if err != nil {
		t.Fatal("exact native task materialization failed:", err)
	}
	materialized.Path, err = privateTaskRoot(root, materialized.Path)
	if err != nil || materialized.Task != *selected || !domain.ValidTaskName(materialized.Name) || !simpleName.MatchString(materialized.TrialPrefix) {
		t.Fatal("materialization did not retain exact source/private identity", err)
	}
	metadata, err := readTaskSourceFile(materialized.Path, "task.toml", 128<<10)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseTaskMetadata(metadata)
	if err != nil {
		t.Fatal("original task metadata is unsupported:", err)
	}
	metadataHash := sha256.Sum256(metadata)
	evidence := map[string]any{"verification": "native_source_and_exact_task_download", "model_inference": false, "benchmark_scores": nil, "dataset": resolved.Suite.Dataset, "dataset_version": resolved.Suite.Version, "dataset_digest": resolved.DatasetDigest, "dataset_revision": resolved.DatasetRevision, "registry_url": resolved.Suite.RegistryURL, "registry_metadata_url": resolved.RegistryMetadataURL, "registry_kind": resolved.RegistryKind, "resolution_hash": resolved.ResolutionHash, "task_count": len(resolved.Tasks), "resolved_at": resolved.ResolvedAt, "task": *selected, "materialized_name": materialized.Name, "trial_prefix": materialized.TrialPrefix, "task_metadata_sha256": hex.EncodeToString(metadataHash[:]), "original_image": parsed.Image, "build_timeout_sec": parsed.BuildTimeoutSec, "verifier_mode": parsed.VerifierMode, "harbor": tools, "ca_profile": environment.CACertPath}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	t.Log(string(data))
	if filename := os.Getenv("ROUTER_FORGE_NATIVE_SOURCE_EVIDENCE"); filename != "" {
		if !filepath.IsAbs(filename) {
			t.Fatal("evidence path must be absolute")
		}
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

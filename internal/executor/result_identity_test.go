package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func writeSourceTrial(t *testing.T, taskName, prefix string, source domain.BenchmarkTask, mutate func(map[string]any)) string {
	t.Helper()
	root := projectTestDir(t)
	trial := prefix + "__fixture"
	directory := filepath.Join(root, trial)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	reference := map[string]any{"name": source.PackageName, "ref": source.Ref}
	if source.Kind == "git" {
		reference = map[string]any{"path": source.Path, "git_url": source.GitURL, "git_commit_id": source.GitCommit}
	}
	result := map[string]any{"task_name": taskName, "trial_name": trial, "finished_at": domain.Now(), "verifier_result": map[string]any{"rewards": map[string]float64{"reward": 0}}, "config": map[string]any{"task": reference}}
	if mutate != nil {
		mutate(result)
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(directory, "result.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNativeTaskNameAndTrialPrefixAreNotGuessedFromPackageName(t *testing.T) {
	source := domain.BenchmarkTask{Name: "swe-bench/canonical-task", Kind: "package", PackageName: "swe-bench/canonical-task", Ref: "sha256:" + strings.Repeat("d", 64)}
	name, prefix := strings.Repeat("d", 64), "canonical-task"
	root := writeSourceTrial(t, name, prefix, source, nil)
	result, err := ReadHarborResultForTask(context.Background(), root, name, source, 0, prefix)
	if err != nil || result.State != "completed" || result.Reward == nil || *result.Reward != 0 {
		t.Fatal("native task name and physical prefix were conflated", err)
	}
	if _, err := ReadHarborResultForTask(context.Background(), root, name, source, 0); !errors.Is(err, ErrResultIdentity) {
		t.Fatal("missing native physical prefix was guessed", err)
	}
}

func TestSourceAwareResultsRejectNamespaceDigestAndTrialSubstitution(t *testing.T) {
	source := domain.BenchmarkTask{Name: "swe-bench/canonical-task", Kind: "package", PackageName: "swe-bench/canonical-task", Ref: "sha256:" + strings.Repeat("d", 64)}
	for _, mutate := range []func(map[string]any){
		func(result map[string]any) {
			result["config"].(map[string]any)["task"].(map[string]any)["name"] = "another-org/canonical-task"
		},
		func(result map[string]any) {
			result["config"].(map[string]any)["task"].(map[string]any)["ref"] = "sha256:" + strings.Repeat("e", 64)
		},
		func(result map[string]any) { result["config"] = map[string]any{} },
		func(result map[string]any) { result["task_name"] = "another-task" },
		func(result map[string]any) { result["trial_name"] = "another-trial" },
	} {
		root := writeSourceTrial(t, "native-task-name", "native-prefix", source, mutate)
		if _, err := ReadHarborResultForTask(context.Background(), root, "native-task-name", source, 0, "native-prefix"); !errors.Is(err, ErrResultIdentity) {
			t.Fatal("substituted task source or identity accepted", err)
		}
	}
}

func TestSourceAwareGitResultRequiresRecordedCommit(t *testing.T) {
	source := fixtureSpec().Snapshot.ResolvedTasks[0]
	root := writeSourceTrial(t, source.Name, source.Name, source, nil)
	if _, err := ReadHarborResultForTask(context.Background(), root, source.Name, source, 0, source.Name); err != nil {
		t.Fatal(err)
	}
	bad := writeSourceTrial(t, source.Name, source.Name, source, func(result map[string]any) {
		result["config"].(map[string]any)["task"].(map[string]any)["git_commit_id"] = strings.Repeat("b", 40)
	})
	if _, err := ReadHarborResultForTask(context.Background(), bad, source.Name, source, 0, source.Name); !errors.Is(err, ErrResultIdentity) {
		t.Fatal("Git task commit substituted", err)
	}
}

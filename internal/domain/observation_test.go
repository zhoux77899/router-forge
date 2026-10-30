package domain

import (
	"strings"
	"testing"
)

func TestNativeMaterializedNameIsBoundToApprovedSourceNotGuessedAlias(t *testing.T) {
	snapshot := snapshotFixture(t)
	task := BenchmarkTask{Name: "swe-bench/canonical-task", Kind: "package", PackageName: "swe-bench/canonical-task", Ref: "sha256:" + strings.Repeat("d", 64)}
	snapshot.Spec.TaskNames, snapshot.ResolvedTasks = []string{task.Name}, []BenchmarkTask{task}
	snapshot.Suite.Version, snapshot.DatasetDigest, snapshot.RegistryKind = "sha256:"+strings.Repeat("e", 64), "sha256:"+strings.Repeat("e", 64), "package"
	snapshot.ConfigHash, _ = snapshot.ApprovalHash()
	tools := ExecutionObservation{Kind: "tools", Timestamp: Now(), WorkDir: "/private/work", Tools: []ToolStatus{{Name: "uv", Path: "/tools/uv", Available: true}, {Name: "harbor", Path: "/tools/harbor", Available: true}}}
	if err := snapshot.ApplyObservation(tools); err != nil {
		t.Fatal(err)
	}
	observation := ExecutionObservation{Kind: "task", Timestamp: Now(), Task: &task, RegistryURL: snapshot.Suite.RegistryURL, TaskPath: "/private/work/task", TaskName: strings.Repeat("d", 64), TrialPrefix: "canonical-task"}
	foreign := task
	foreign.Name, foreign.PackageName = "different-org/canonical-task", "different-org/canonical-task"
	observation.Task = &foreign
	if err := snapshot.ApplyObservation(observation); err == nil {
		t.Fatal("another namespace with the same basename was accepted")
	}
	observation.Task = &task
	if err := snapshot.ApplyObservation(observation); err != nil {
		t.Fatal("native digest basename was incorrectly replaced by a guessed canonical alias", err)
	}
	if snapshot.Runtime.TaskName != strings.Repeat("d", 64) || snapshot.Runtime.TrialPrefix != "canonical-task" {
		t.Fatal("native naming observations were lost")
	}
	if err := snapshot.ApplyObservation(ExecutionObservation{Kind: "agent", Timestamp: Now(), AgentVersion: "2.4.0"}); err == nil {
		t.Fatal("unobserved claimed version without CLI output accepted")
	}
	if err := snapshot.ApplyObservation(ExecutionObservation{Kind: "agent", Timestamp: Now(), AgentVersion: "2.4.0", AgentVersionOutput: "opencode 2.4.0"}); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.ValidateSources(); err != nil {
		t.Fatal("runtime naming changed approval", err)
	}
}

func TestUnavailableToolObservationIsRecordedButCannotAdmitAgent(t *testing.T) {
	snapshot := snapshotFixture(t)
	tools := ExecutionObservation{Kind: "tools", Timestamp: Now(), WorkDir: "/private/work", Tools: []ToolStatus{{Name: "uv", Available: false}, {Name: "harbor", Path: "/tools/harbor", Available: true}}}
	if err := snapshot.ApplyObservation(tools); err != nil {
		t.Fatal(err)
	}
	task := snapshot.ResolvedTasks[0]
	observation := ExecutionObservation{Kind: "task", Timestamp: Now(), Task: &task, RegistryURL: snapshot.Suite.RegistryURL, TaskPath: "/private/work/task", TaskName: task.Name, TrialPrefix: task.Name}
	if err := snapshot.ApplyObservation(observation); err == nil {
		t.Fatal("missing uv admitted materialization")
	}
	if snapshot.Runtime == nil || snapshot.Runtime.TaskPath != "" || snapshot.Runtime.Tools[0].Available {
		t.Fatal("failed tool probe was lost or fabricated")
	}
}

package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestObservedAgentVersionUsesSuccessfulCLIOutputNotRequestedVersion(t *testing.T) {
	for _, test := range []struct{ output, version string }{
		{"opencode v2.4.0", "2.4.0"},
		{"opencode custom development build", "opencode custom development build"},
		{"opencode 2.4.0 (unknown git commit)", "2.4.0"},
		{"", ""}, {"unknown", ""}, {"Error: expected version 2.4.0", ""},
		{"usage: agent --version 2.4.0", ""}, {"sh: command not found", ""},
		{"opencode: not found", ""}, {"opencode: no such file or directory", ""},
		{"version\x00 2.4.0", ""}, {string([]byte{0xff}), ""},
	} {
		actual, _ := observedAgentVersion([]byte(test.output))
		if actual != test.version {
			t.Errorf("%q: got %q, want %q", test.output, actual, test.version)
		}
	}
}

func TestRecordObservationDoesNotMutateSnapshotBeforeDurableAcknowledgment(t *testing.T) {
	spec := fixtureSpec()
	observation := fixtureObservations(spec, domain.Environment{WorkDir: projectTestDir(t)})[0]
	if err := recordObservation(&spec.Snapshot, func(domain.ExecutionObservation) error { return errors.New("fixture persistence failure") }, observation); err == nil || spec.Snapshot.Runtime != nil {
		t.Fatal("failed acknowledgment mutated execution snapshot")
	}
	if err := recordObservation(&spec.Snapshot, acknowledgeFixtureObservation, observation); err != nil || spec.Snapshot.Runtime == nil {
		t.Fatal("successful acknowledgment was not retained", err)
	}
}

func TestPrivateObservationsRejectForeignDuplicateAndLateReceipts(t *testing.T) {
	spec := fixtureSpec()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	pending := &pendingCommand{execute: true, token: spec.GatewayToken, spec: &spec, ctx: ctx, observe: func(domain.ExecutionObservation) error { calls.Add(1); return nil }}
	session := &workerSession{manager: &Manager{events: make(chan callback, 1)}, pending: map[string]*pendingCommand{"cmd-fixture": pending}}
	observations := fixtureObservations(spec, domain.Environment{WorkDir: projectTestDir(t)})
	post := func(value observationReply) int {
		body, _ := json.Marshal(value)
		r := httptest.NewRequest(http.MethodPost, "/observations", bytes.NewReader(body))
		r.Header.Set("X-Router-Forge-Version", domain.Version)
		w := httptest.NewRecorder()
		session.ServeHTTP(w, r)
		return w.Code
	}
	value := observationReply{ID: "cmd-fixture", RunID: spec.RunID, TaskID: spec.TaskID, AttemptID: "foreign-attempt", Observation: observations[0]}
	if code := post(value); code != http.StatusConflict || calls.Load() != 0 {
		t.Fatal("foreign receipt admitted", code)
	}
	value.AttemptID = spec.AttemptID
	value.Observation = observations[2]
	if code := post(value); code != http.StatusBadRequest || pending.agentReady.Load() {
		t.Fatal("agent receipt admitted before source observations", code)
	}
	for _, observation := range observations {
		value.Observation = observation
		if code := post(value); code != http.StatusNoContent {
			t.Fatal("valid receipt rejected", code)
		}
		if code := post(value); code != http.StatusBadRequest {
			t.Fatal("duplicate receipt replaced evidence", code)
		}
	}
	if calls.Load() != 3 || !pending.agentReady.Load() {
		t.Fatal("acknowledged agent did not open private gate")
	}
	cancel()
	if code := post(value); code != http.StatusConflict || calls.Load() != 3 {
		t.Fatal("cancelled command accepted a late receipt", code)
	}
}

func TestHTTP2AgentReceiptIsIndependentOfFullEventQueueAndBlocksInference(t *testing.T) {
	var calls atomic.Int32
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "fixture-response")
	}))
	blocked, releaseEvent := make(chan struct{}), make(chan struct{})
	defer close(releaseEvent)
	manager.events <- callback{emit: func(domain.WorkerEvent) { close(blocked); <-releaseEvent }}
	<-blocked
	for range cap(manager.events) {
		manager.events <- callback{emit: func(domain.WorkerEvent) {}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env := domain.Environment{ID: "receipt-queue-fixture", Type: "local", WorkDir: projectTestDir(t)}
	entered, acknowledge := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-acknowledge:
		default:
			close(acknowledge)
		}
	}()
	finished := make(chan error, 1)
	go func() {
		result, err := manager.Execute(ctx, env, transportFixtureSpec(env, "stream-fixture", "fixture-token"), nil, func(observation domain.ExecutionObservation) error {
			if observation.Kind == "agent" {
				close(entered)
				select {
				case <-acknowledge:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
		if err == nil && result.State != "completed" {
			err = errors.New("fixture did not complete")
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if calls.Load() != 0 {
		t.Fatal("inference preceded acknowledgment")
	}
	close(acknowledge)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if calls.Load() != 1 {
		t.Fatal("event backpressure lost the independent agent acknowledgment")
	}
}

func TestHTTP2PersistenceFailureNeverForwardsInference(t *testing.T) {
	var calls atomic.Int32
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env := domain.Environment{ID: "receipt-failure-fixture", Type: "local", WorkDir: projectTestDir(t)}
	_, err := manager.Execute(ctx, env, transportFixtureSpec(env, "stream-fixture", "fixture-token"), nil, func(observation domain.ExecutionObservation) error {
		if observation.Kind == "agent" {
			return errors.New("fixture durable storage failure")
		}
		return nil
	})
	if err == nil || calls.Load() != 0 {
		t.Fatal("failed receipt authorized model forwarding")
	}
}

func TestContainerProbeStartsAtFirstRequestAndWaitsForDurableReceipt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "failed"}[fail], func(t *testing.T) {
			spec := fixtureSpec()
			root := projectTestDir(t)
			tracker := newDockerTracker("fixture-docker", nil, "owner_fixture", root, filepath.Join(root, "jobs"), spec)
			container := DockerContainer{ID: strings.Repeat("a", 64), Image: "sha256:" + strings.Repeat("1", 64), Running: true, Labels: map[string]string{"io.router-forge.owner": tracker.owner, "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}}
			var probes atomic.Int32
			tracker.runner = func(ctx context.Context, command Command, env []string, dir string, input io.Reader, limit int) ([]byte, int, error) {
				switch command.Args[0] {
				case "ps":
					return []byte(container.ID), 0, nil
				case "inspect":
					data, _ := json.Marshal(container)
					return data, 0, nil
				case "exec":
					probes.Add(1)
					return []byte("opencode 2.4.0"), 0, nil
				}
				return nil, 0, nil
			}
			entered, receipt := make(chan struct{}), make(chan struct{})
			tracker.observer = func(observation domain.ExecutionObservation) error {
				if observation.AgentVersion != "2.4.0" || observation.AgentVersionOutput == "" {
					t.Error("probe did not record independent actual output")
				}
				close(entered)
				<-receipt
				if fail {
					return errors.New("fixture durable storage failure")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go tracker.observe(ctx, cancel, done)
			time.Sleep(350 * time.Millisecond)
			if probes.Load() != 0 {
				t.Fatal("preinstalled image CLI was probed before Harbor's default setup could finish")
			}
			tracker.requestProbe()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("container probe did not start")
			}
			select {
			case <-tracker.ready:
				t.Fatal("gate opened before durable acknowledgment")
			default:
			}
			close(receipt)
			if fail {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("failed receipt did not stop job")
				}
				select {
				case <-tracker.ready:
					t.Fatal("failed acknowledgment opened gate")
				default:
				}
				version, _, err := tracker.snapshot()
				if version != "" || err == nil {
					t.Fatal("failed receipt was recorded as actual version")
				}
			} else {
				select {
				case <-tracker.ready:
				case <-time.After(3 * time.Second):
					t.Fatal("successful receipt did not open gate")
				}
				cancel()
				<-done
				version, _, err := tracker.snapshot()
				if version != "2.4.0" || err != nil {
					t.Fatal("actual version was not retained")
				}
			}
		})
	}
}

func TestNativeTaskRootRejectsEscapingSymlinksAndReadsOriginalFiles(t *testing.T) {
	root := projectTestDir(t)
	task := filepath.Join(root, "task")
	if err := os.Mkdir(task, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("[verifier]\ncommand = 'original verifier'\n")
	if err := os.WriteFile(filepath.Join(task, "task.toml"), original, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := privateTaskRoot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	data, err := readTaskSourceFile(resolved, "task.toml", 128<<10)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("original task file changed", err)
	}
	if _, err := privateTaskRoot(root, filepath.Dir(root)); err == nil {
		t.Fatal("outside task root accepted")
	}
	outside := filepath.Join(projectTestDir(t), "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked-task")
	if err := os.Symlink(outside, link); err != nil {
		t.Log("symlink creation unavailable on this host")
		return
	}
	if _, err := privateTaskRoot(root, link); err == nil {
		t.Fatal("escaping task symlink accepted")
	}
}

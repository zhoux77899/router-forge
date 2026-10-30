package executor

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = io.WriteString(os.Stdout, "fixture development build\n")
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "--stdio" {
		err := ServeWorker(context.Background(), os.Stdin, os.Stdout, func(c *http.Client) WorkerBackend { return fixtureBackend{client: c} })
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fixtureBackend struct{ client *http.Client }

func (f fixtureBackend) Inspect(ctx context.Context, e domain.Environment) (domain.Inspection, error) {
	return domain.Inspection{EnvironmentID: e.ID, OS: runtime.GOOS, Arch: runtime.GOARCH, Ready: true, WorkDir: e.WorkDir, HomeDir: strconv.Itoa(os.Getpid())}, nil
}
func (f fixtureBackend) Resolve(ctx context.Context, e domain.Environment, suite domain.Suite) (domain.ResolvedBenchmark, error) {
	s := fixtureSpec().Snapshot
	resolved := domain.ResolvedBenchmark{Suite: s.Suite, RegistryKind: s.RegistryKind, ResolvedAt: s.ResolvedAt, ResolutionHash: s.ResolutionHash, Tasks: s.ResolvedTasks}
	return resolved, nil
}
func (f fixtureBackend) Tasks(ctx context.Context, e domain.Environment, suite domain.Suite) ([]domain.BenchmarkTask, error) {
	resolved, err := f.Resolve(ctx, e, suite)
	return resolved.Tasks, err
}
func (f fixtureBackend) Execute(ctx context.Context, e domain.Environment, s domain.ExecutionSpec, emit func(domain.WorkerEvent), observe func(domain.ExecutionObservation) error) (domain.ExecutionResult, error) {
	if s.JobName == "disconnect-fixture" {
		os.Exit(7)
	}
	if s.JobName == "cancel-fixture" {
		emit(domain.WorkerEvent{Kind: "running", Message: "fixture active"})
		<-ctx.Done()
		return domain.ExecutionResult{State: "cancelled", ExitCode: -1}, ctx.Err()
	}
	for _, observation := range fixtureObservations(s, e) {
		if err := observe(observation); err != nil {
			return domain.ExecutionResult{State: "failed", ExitCode: -1, ExceptionType: "ObservationError"}, err
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://router-forge/v1/chat/completions", strings.NewReader(`{"model":"fixture","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+s.GatewayToken)
	resp, err := f.client.Do(req)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	emit(domain.WorkerEvent{Kind: "running", Message: "fixture gateway stream complete"})
	return domain.ExecutionResult{State: "completed", ExitCode: 0, ResultPath: string(b)}, nil
}

func transportFixtureSpec(env domain.Environment, job, token string) domain.ExecutionSpec {
	spec := fixtureSpec()
	spec.JobName, spec.GatewayToken = job, token
	spec.Snapshot.Environment = env
	spec.Snapshot.Spec.EnvironmentID = env.ID
	sealFixtureSpec(&spec)
	return spec
}

func acknowledgeFixtureObservation(domain.ExecutionObservation) error { return nil }

func fixtureObservations(spec domain.ExecutionSpec, env domain.Environment) []domain.ExecutionObservation {
	return []domain.ExecutionObservation{
		{Kind: "tools", Timestamp: domain.Now(), WorkDir: env.WorkDir, Tools: []domain.ToolStatus{{Name: "uv", Path: "fixture-uv", Available: true, Version: "8.7.6", VersionOutput: "uv 8.7.6"}, {Name: "harbor", Path: "fixture-harbor", Available: true, Version: "9.8.7", VersionOutput: "harbor 9.8.7"}}},
		{Kind: "task", Timestamp: domain.Now(), Task: &spec.Snapshot.ResolvedTasks[0], RegistryURL: spec.Snapshot.Suite.RegistryURL, TaskPath: filepath.Join(env.WorkDir, "task"), TaskName: spec.Snapshot.Spec.TaskNames[0], TrialPrefix: spec.Snapshot.Spec.TaskNames[0]},
		{Kind: "agent", Timestamp: domain.Now(), AgentVersion: "2.4.0", AgentVersionOutput: "opencode 2.4.0"},
	}
}

func projectTestDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("test-work")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, domain.NewID("fixture"))
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(time.Second)
		for {
			err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err == nil && entry.Type().IsRegular() {
					return os.Chmod(path, 0600)
				}
				return err
			})
			if err == nil || os.IsNotExist(err) {
				err = os.RemoveAll(dir)
			}
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("remove owned fixture directory: %v", err)
				break
			}
			// Windows may briefly retain an exited helper's executable mapping.
			time.Sleep(10 * time.Millisecond)
		}
		_ = os.Remove(root)
	})
	return dir
}
func helperManager(t *testing.T, h http.Handler) *Manager {
	t.Helper()
	dir := projectTestDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	name, _ := WorkerFilename(runtime.GOOS, runtime.GOARCH)
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	manager := New(dir, h)
	manager.localRoot = filepath.Join(dir, "deployed")
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestManagerRPCMultiplexesGatewayStreamingWithControl(t *testing.T) {
	first := make(chan struct{})
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-lease" {
			t.Error("lease credential not preserved")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		close(first)
		time.Sleep(80 * time.Millisecond)
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env := domain.Environment{ID: "fixture-local", Type: "local", WorkDir: projectTestDir(t)}
	inspected, err := manager.Inspect(ctx, env)
	if err != nil || !inspected.Ready {
		t.Fatalf("inspect: %#v %v", inspected, err)
	}
	finished := make(chan domain.ExecutionResult, 1)
	go func() {
		r, err := manager.Execute(ctx, env, transportFixtureSpec(env, "stream-fixture", "fixture-lease"), nil, acknowledgeFixtureObservation)
		if err != nil {
			t.Error(err)
		}
		finished <- r
	}()
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	tasks, err := manager.Tasks(ctx, env, domain.SWEBenchVerified())
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks during SSE: %#v %v", tasks, err)
	}
	select {
	case result := <-finished:
		if result.ResultPath != "data: first\n\ndata: second\n\n" {
			t.Fatalf("stream changed: %q", result.ResultPath)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestManagerCancellationStopsOnlyOwnedCommandAndKeepsSession(t *testing.T) {
	manager := helperManager(t, nil)
	env := domain.Environment{ID: "fixture-cancel", Type: "local", WorkDir: projectTestDir(t)}
	ctx, cancel := context.WithCancel(context.Background())
	running := make(chan struct{}, 1)
	finished := make(chan domain.ExecutionResult, 1)
	go func() {
		r, _ := manager.Execute(ctx, env, transportFixtureSpec(env, "cancel-fixture", "fixture-token"), func(e domain.WorkerEvent) {
			if e.Kind == "running" {
				running <- struct{}{}
			}
		}, acknowledgeFixtureObservation)
		finished <- r
	}()
	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not begin")
	}
	cancel()
	select {
	case result := <-finished:
		if result.State != "cancelled" {
			t.Fatalf("state = %s", result.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel acknowledgement stalled")
	}
	probeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, err := manager.Inspect(probeCtx, env); err != nil {
		t.Fatalf("cancel broke independent worker session: %v", err)
	}
}

package executor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestSessionIdentityIncludesProfilesAndPreservesFrozenConnections(t *testing.T) {
	manager := helperManager(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env := domain.Environment{ID: "same-environment", Type: "local", WorkDir: projectTestDir(t)}
	results := make(chan domain.Inspection, 2)
	for range 2 {
		go func() {
			r, err := manager.Inspect(ctx, env)
			if err != nil {
				t.Error(err)
			}
			results <- r
		}()
	}
	first, second := <-results, <-results
	if first.HomeDir == "" || first.HomeDir != second.HomeDir {
		t.Fatal("same profile started duplicate worker processes")
	}
	changed := env
	changed.ProxyURL = "http://fixture-proxy.example:3128"
	changed.NoProxy = "registry.example"
	fresh, err := manager.Inspect(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.HomeDir == first.HomeDir {
		t.Fatal("changed profile reused a stale connection")
	}
	original, err := manager.Inspect(ctx, env)
	if err != nil || original.HomeDir != first.HomeDir {
		t.Fatal("profile edit destroyed the frozen original connection")
	}
}
func TestConnectionFailureNeverRerunsPaidCommand(t *testing.T) {
	var calls atomic.Int32
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env := domain.Environment{ID: "broken-worker", Type: "local", WorkDir: projectTestDir(t)}
	original, err := manager.Inspect(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Execute(ctx, env, transportFixtureSpec(env, "disconnect-fixture", "fixture-token"), nil, acknowledgeFixtureObservation)
	if !errors.Is(err, ErrWorkerConnection) || result.State != "interrupted" || calls.Load() != 0 {
		t.Fatalf("disconnect = %#v %v calls=%d", result, err, calls.Load())
	}
	fresh, err := manager.Inspect(ctx, env)
	if err != nil || fresh.HomeDir == original.HomeDir {
		t.Fatalf("explicit inspection did not reconnect: %#v %v", fresh, err)
	}
	if calls.Load() != 0 {
		t.Fatal("reconnection reran an execution")
	}
}
func TestSlowCallbackDoesNotBlockResultOrGatewayControl(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("fixture-response")) }))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := domain.Environment{ID: "slow-callback", Type: "local", WorkDir: projectTestDir(t)}
	result, err := manager.Execute(ctx, env, transportFixtureSpec(env, "stream-fixture", "fixture-token"), func(domain.WorkerEvent) { <-unblock }, acknowledgeFixtureObservation)
	if err != nil || result.State != "completed" {
		t.Fatalf("blocked callback stalled RPC: %#v %v", result, err)
	}
	if _, err = manager.Inspect(ctx, env); err != nil {
		t.Fatal(err)
	}
}
func TestCancellationPropagatesAcrossRealHTTP2GatewayStream(t *testing.T) {
	entered := make(chan struct{})
	aborted := make(chan struct{})
	manager := helperManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(aborted) }))
	env := domain.Environment{ID: "stream-abort", Type: "local", WorkDir: projectTestDir(t)}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		_, _ = manager.Execute(ctx, env, transportFixtureSpec(env, "stream-fixture", "fixture-token"), nil, acknowledgeFixtureObservation)
		close(finished)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model stream did not enter")
	}
	cancel()
	select {
	case <-aborted:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway request was not aborted")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled command did not finish")
	}
}
func TestManagerRejectsConflictingLeaseCredentials(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "http://router-forge/v1/messages", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer one")
	request.Header.Set("x-api-key", "two")
	if _, ok := leaseHeader(request); ok {
		t.Fatal("conflicting native credentials accepted")
	}
}

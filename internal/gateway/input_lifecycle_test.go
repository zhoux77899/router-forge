package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

type stalledInput struct {
	once      sync.Once
	closeOnce sync.Once
	started   chan struct{}
	closed    chan struct{}
}

func (b *stalledInput) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *stalledInput) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }

func TestLeaseTimeoutAndRevocationAlsoBoundAcceptedBodyReads(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name, state := "timeout", "timed_out"
		if revoke {
			name, state = "revocation", "canceled"
		}
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int64
			g, ledger, lease := fixture(t, domain.ChatCompletions, func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }, func(lease *gateway.Lease) { lease.Timeout = 80 * time.Millisecond })
			body := &stalledInput{started: make(chan struct{}), closed: make(chan struct{})}
			defer body.Close()
			r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			r.Body, r.ContentLength = body, -1
			r.Header.Set("Authorization", "Bearer "+lease.Token)
			done := make(chan struct{})
			go func() { g.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
			select {
			case <-body.started:
			case <-time.After(time.Second):
				t.Fatal("body read did not start")
			}
			if revoke {
				g.Revoke(lease.Token)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("lease did not cancel/bound input body read")
			}
			if hits.Load() != 0 || len(ledger.records()) != 1 || ledger.records()[0].State != state || ledger.finalContextErr != nil {
				t.Fatalf("canceled input not safely audited: %+v", ledger.records())
			}
		})
	}
}

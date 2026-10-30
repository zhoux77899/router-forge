package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/netpolicy"
)

type closingTransport struct {
	http.RoundTripper
	closed atomic.Int64
}

func (transport *closingTransport) CloseIdleConnections() {
	transport.closed.Add(1)
	if closer, ok := transport.RoundTripper.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func TestLeaseNetworkSnapshotAndOwnedTransportLifecycle(t *testing.T) {
	original := http.DefaultTransport
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("registration must not make a model request") }))
	t.Cleanup(upstream.Close)
	profile := &domain.NetworkProfile{
		ID: "desktop-profile", Revision: "profile-revision-1", Name: "Explicit Desktop network",
		Scope: domain.NetworkDesktop, ProxyMode: domain.NetworkProxy, ProxyURL: upstream.URL,
		ProxyTargets: []string{"all"}, ProxyAuth: domain.NetworkBasicAuth, RelayMode: domain.NetworkLoopback,
		HasCredentials: true, CredentialRevision: "credential-revision-1",
		TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA, CreatedAt: "2026-10-29T00:00:00Z",
	}
	credential := &domain.ProxyCredential{Username: "first-private-user", Password: "first-private-password"}
	lease := Lease{
		Token: "first-lease", RunID: "run", TaskID: "task", AttemptID: "first-attempt",
		Provider: domain.Provider{ID: "provider", BaseURL: upstream.URL},
		Model:    domain.Model{ID: "model", ProviderID: "provider", Name: "fixed-model", Protocol: domain.ChatCompletions},
		APIKey:   "private-upstream-key", Network: profile, ProxyCredential: credential,
		MaxRequests: 1, MaxOutputTokens: 1, Timeout: time.Second,
	}
	g := New(nil)
	t.Cleanup(func() { _ = g.Close() })
	if _, err := g.Register(lease); err != nil {
		t.Fatal(err)
	}
	first := g.leases[lease.Token]
	profile.Revision, profile.CredentialRevision = "profile-revision-2", "credential-revision-2"
	profile.ProxyTargets[0], profile.NoProxy = "https", "*"
	credential.Username, credential.Password = "second-private-user", "second-private-password"
	if first.lease.Network == profile || first.lease.ProxyCredential == credential || first.lease.Network.Revision != "profile-revision-1" || first.lease.Network.CredentialRevision != "credential-revision-1" || first.lease.Network.ProxyTargets[0] != "all" || first.lease.Network.NoProxy != "" || first.lease.ProxyCredential.Username != "first-private-user" || first.lease.ProxyCredential.Password != "first-private-password" {
		t.Fatal("registered lease retained caller-owned network settings or credentials")
	}
	lease.Token, lease.AttemptID = "second-lease", "second-attempt"
	if _, err := g.Register(lease); err != nil {
		t.Fatal(err)
	}
	second := g.leases[lease.Token]
	if first.client == second.client || first.client.Transport == second.client.Transport || first.client.Transport == original || second.client.Transport == original {
		t.Fatal("lease clients or transports were shared")
	}
	if _, ok := first.client.Transport.(*netpolicy.Transport); !ok {
		t.Fatal("gateway did not use the isolated explicit-network transport")
	}
	firstTransport := &closingTransport{RoundTripper: first.client.Transport}
	secondTransport := &closingTransport{RoundTripper: second.client.Transport}
	first.client.Transport, second.client.Transport = firstTransport, secondTransport
	firstContext, firstCancel := context.WithCancel(context.Background())
	secondContext, secondCancel := context.WithCancel(context.Background())
	defer firstCancel()
	defer secondCancel()
	first.active["first-request"], second.active["second-request"] = firstCancel, secondCancel
	g.Revoke("first-lease")
	if firstContext.Err() != context.Canceled || secondContext.Err() != nil || firstTransport.closed.Load() != 1 || secondTransport.closed.Load() != 0 {
		t.Fatal("revocation did not exclusively release its owned transport and requests")
	}
	if err := g.Close(); err != nil || secondContext.Err() != context.Canceled || secondTransport.closed.Load() != 1 {
		t.Fatal("gateway close did not release every remaining lease")
	}
	g.Revoke("first-lease")
	if err := g.Close(); err != nil || firstTransport.closed.Load() != 1 || secondTransport.closed.Load() != 1 {
		t.Fatal("transport lifecycle was not idempotent")
	}
	lease.Token = "closed-gateway-lease"
	if _, err := g.Register(lease); err == nil {
		t.Fatal("a closed gateway admitted a new network lease")
	}
	if http.DefaultTransport != original {
		t.Fatal("gateway lifecycle modified the shared transport")
	}
}

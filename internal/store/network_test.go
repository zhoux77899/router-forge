package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func storeNetworkFixture(scope domain.NetworkScope, environmentID string) domain.NetworkProfile {
	p := domain.NetworkProfile{Name: "explicit network", Scope: scope, EnvironmentID: environmentID, ProxyMode: domain.NetworkDirect, TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA}
	if scope == domain.NetworkEnvironment {
		p.Mirrors = &domain.NetworkMirrors{APT: domain.NetworkSource{Mode: domain.NetworkNative}, PyPI: domain.NetworkSource{Mode: domain.NetworkNative}, NPM: domain.NetworkSource{Mode: domain.NetworkNative}, Node: domain.NetworkSource{Mode: domain.NetworkNative}, UV: domain.NetworkSource{Mode: domain.NetworkNative}, UVPython: domain.NetworkSource{Mode: domain.NetworkNative}}
	}
	return p
}

func TestNetworkMigrationDoesNotInventProfilesOrBindings(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profiles, err := s.NetworkProfiles(ctx)
	if err != nil || len(profiles) != 0 {
		t.Fatal("migration created network defaults", err)
	}
	environment, err := s.SaveEnvironment(ctx, domain.Environment{Name: "legacy", Type: "local", ProxyURL: "https://proxy.example", CACertPath: "/legacy.pem", SkipTLSVerification: true})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Environment(ctx, environment.ID)
	if err != nil || loaded.Networks != nil || loaded.DockerGatewayMode != "" || !loaded.SkipTLSVerification || loaded.CACertPath != "/legacy.pem" {
		t.Fatal("legacy data was replaced or profiled", err)
	}
	if _, err = s.SourceEnvironment(ctx, loaded); err == nil {
		t.Fatal("unselected legacy source network was usable")
	}
	if _, err = s.SaveEnvironment(ctx, domain.Environment{Name: "ssh", Type: "ssh", Host: "configured-alias"}); err == nil {
		t.Fatal("SSH port was silently filled")
	}
}

func TestNetworkProfileAndProxyCredentialsHaveIndependentImmutableRevisions(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := storeNetworkFixture(domain.NetworkDesktop, "")
	p.ProxyMode, p.ProxyURL, p.ProxyAuth, p.ProxyTargets, p.RelayMode = domain.NetworkProxy, "https://proxy.example:8443", domain.NetworkBasicAuth, []string{"https"}, domain.NetworkLoopback
	username, password := "private-proxy-user", "private-proxy-password"
	first, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: p, ProxyUsername: &username, ProxyPassword: &password})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasCredentials || first.CredentialRevision == "" || first.Revision == "" {
		t.Fatal("credential metadata absent")
	}
	public, _ := json.Marshal(first)
	if strings.Contains(string(public), username) || strings.Contains(string(public), password) {
		t.Fatal("proxy credential leaked into public metadata")
	}
	secondInput := first
	secondInput.Name = "edited network"
	second, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: secondInput})
	if err != nil || first.Revision == second.Revision || first.CredentialRevision != second.CredentialRevision {
		t.Fatal("omitted credentials were not retained with an independent profile revision", err)
	}
	history, err := s.NetworkProfileRevision(ctx, first.ID, first.Revision)
	if err != nil || history.Name != first.Name || history.Revision != first.Revision {
		t.Fatal("frozen profile was mutated", err)
	}
	if _, err = s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: first}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale editor revision was accepted", err)
	}
	newPassword := "replacement-password"
	third, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: second, ProxyPassword: &newPassword})
	if err != nil || third.CredentialRevision == second.CredentialRevision {
		t.Fatal("credential update did not create a new revision", err)
	}
	oldCredential, err := s.ProxyCredential(ctx, first.ID, first.CredentialRevision)
	if err != nil || oldCredential.Username != username || oldCredential.Password != password {
		t.Fatal("old approved credential was changed", err)
	}
	newCredential, err := s.ProxyCredential(ctx, third.ID, third.CredentialRevision)
	if err != nil || newCredential.Password != newPassword || newCredential.Username != username {
		t.Fatal("partial replacement did not retain omitted username", err)
	}
	if _, err := s.ProxyCredential(ctx, "another-profile", third.CredentialRevision); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("credential revision was usable by another owner", err)
	}
	clear := third
	clear.ProxyMode, clear.ProxyURL, clear.ProxyAuth, clear.ProxyTargets, clear.RelayMode = domain.NetworkDirect, "", "", nil, ""
	cleared, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: clear, ClearCredentials: true})
	if err != nil || cleared.HasCredentials || cleared.CredentialRevision != "" {
		t.Fatal("clear retained an active credential", err)
	}
	for _, revision := range []string{first.CredentialRevision, third.CredentialRevision} {
		if _, err := s.ProxyCredential(ctx, first.ID, revision); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("clear did not invalidate historical references", err)
		}
	}
}

func TestNetworkRiskConfirmationIsServerDerivedAndCAIsRetained(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := storeNetworkFixture(domain.NetworkDesktop, "")
	p.TLSMode, p.TrustMode, p.CACertPath, p.TLSRiskAccepted = domain.NetworkSkipTLS, domain.NetworkCustomCA, "/missing/retained.pem", true
	if _, err = s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: p}); err == nil {
		t.Fatal("client-supplied confirmation metadata was trusted")
	}
	first, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: p, ConfirmTLSRisk: true})
	if err != nil || !first.TLSRiskAccepted || first.CACertPath != p.CACertPath {
		t.Fatal("explicit risk/ignored CA not retained", err)
	}
	if _, err = s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: first}); err == nil {
		t.Fatal("old confirmation was reused by a save")
	}
	first.TLSMode = domain.NetworkVerifyTLS
	strict, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: first})
	if err != nil || strict.TLSRiskAccepted || strict.CACertPath != p.CACertPath {
		t.Fatal("strict restoration lost CA or retained risk", err)
	}
}

func TestNetworkHostBindingsFreezeWithoutLatestProfileOrModelSecretInheritance(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	environment, err := s.SaveEnvironment(ctx, domain.Environment{Name: "execution", Type: "local", DockerGatewayMode: "detect", ProxyURL: "https://legacy.example", SkipTLSVerification: true})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: storeNetworkFixture(domain.NetworkEnvironment, environment.ID)})
	if err != nil {
		t.Fatal(err)
	}
	modelProfile := storeNetworkFixture(domain.NetworkDesktop, "")
	modelProfile.ProxyMode, modelProfile.ProxyURL, modelProfile.ProxyAuth, modelProfile.ProxyTargets, modelProfile.RelayMode = domain.NetworkProxy, "https://proxy.example", domain.NetworkBasicAuth, []string{"https"}, domain.NetworkLoopback
	username, password := "model-only-user", "model-only-password"
	modelNetwork, err := s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: modelProfile, ProxyUsername: &username, ProxyPassword: &password})
	if err != nil {
		t.Fatal(err)
	}
	environment.Networks = &domain.EnvironmentNetworks{SourceProfileID: execution.ID, BuildProfileID: execution.ID, AgentProfileID: execution.ID, VerifierProfileID: execution.ID}
	environment, err = s.SaveEnvironment(ctx, environment)
	if err != nil {
		t.Fatal(err)
	}
	networkID, key := modelNetwork.ID, "provider-key"
	provider, err := s.SaveProvider(ctx, domain.ProviderInput{Name: "model", BaseURL: "https://provider.example/v1", APIKey: &key, NetworkProfileID: &networkID})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := s.FreezeNetwork(ctx, environment, provider)
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.ExecutionNetworks(ctx, frozen)
	if err != nil || len(private.Credentials) != 0 {
		t.Fatal("Desktop model proxy credential crossed into worker context", err)
	}
	runtime, err := s.SourceEnvironment(ctx, environment)
	if err != nil || runtime.NetworkRuntime == nil || runtime.ProxyURL != "" || runtime.SkipTLSVerification {
		t.Fatal("legacy settings remained active in an explicitly profiled source operation", err)
	}
	modelNetwork.Name = "later edit"
	if _, err = s.SaveNetworkProfile(ctx, domain.NetworkProfileInput{Profile: modelNetwork}); err != nil {
		t.Fatal(err)
	}
	if frozen.Model.Revision != modelNetwork.Revision || frozen.Model.Name != "explicit network" {
		t.Fatal("later edit mutated the frozen snapshot")
	}
	foreign := environment
	foreign.ID = "another-host"
	if _, err := s.SourceEnvironment(ctx, foreign); err == nil {
		t.Fatal("execution network was borrowed by another host")
	}
	provider, err = s.SaveProvider(ctx, domain.ProviderInput{ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL})
	if err != nil || provider.NetworkProfileID != networkID {
		t.Fatal("omitted binding did not retain its selected network", err)
	}
	empty := ""
	provider, err = s.SaveProvider(ctx, domain.ProviderInput{ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL, NetworkProfileID: &empty})
	if err != nil || provider.NetworkProfileID != "" {
		t.Fatal("explicit clear did not remove provider binding", err)
	}
	if _, err = s.FreezeNetwork(ctx, environment, provider); err == nil {
		t.Fatal("model network was inferred after explicit clear")
	}
}

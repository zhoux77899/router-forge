package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func networkFixture(scope NetworkScope, environmentID, name string) NetworkProfile {
	profile := NetworkProfile{ID: "net_" + name, Revision: "revision_" + name, Name: name, Scope: scope, EnvironmentID: environmentID, ProxyMode: NetworkDirect, TLSMode: NetworkVerifyTLS, TrustMode: NetworkSystemCA, CreatedAt: Now()}
	if scope == NetworkEnvironment {
		profile.Mirrors = &NetworkMirrors{APT: NetworkSource{Mode: NetworkNative}, PyPI: NetworkSource{Mode: NetworkNative}, NPM: NetworkSource{Mode: NetworkNative}, Node: NetworkSource{Mode: NetworkNative}, UV: NetworkSource{Mode: NetworkNative}, UVPython: NetworkSource{Mode: NetworkNative}}
	}
	return profile
}

func TestNetworkModesCannotBeInferredFromZeroValues(t *testing.T) {
	for name, change := range map[string]func(*NetworkProfile){
		"scope": func(p *NetworkProfile) { p.Scope = "" }, "route": func(p *NetworkProfile) { p.ProxyMode = "" },
		"tls": func(p *NetworkProfile) { p.TLSMode = "" }, "trust": func(p *NetworkProfile) { p.TrustMode = "" },
		"host": func(p *NetworkProfile) { p.EnvironmentID = "" }, "mirrors": func(p *NetworkProfile) { p.Mirrors = nil },
		"mirror-choice": func(p *NetworkProfile) { p.Mirrors.UVPython.Mode = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := networkFixture(NetworkEnvironment, "env", "explicit")
			change(&p)
			if p.Validate() == nil {
				t.Fatal("missing selection accepted")
			}
		})
	}
	if err := networkFixture(NetworkDesktop, "", "explicit").ValidateSaved(); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkHostAndProxyCredentialBoundaries(t *testing.T) {
	p := networkFixture(NetworkDesktop, "", "model")
	p.EnvironmentID = "env"
	if p.Validate() == nil {
		t.Fatal("Desktop accepted another host's scope")
	}
	p.EnvironmentID = ""
	p.ProxyMode, p.ProxyURL, p.ProxyTargets = NetworkProxy, "https://proxy.example:8443", []string{"https"}
	if p.Validate() == nil {
		t.Fatal("authentication mode was inferred")
	}
	p.ProxyAuth = NetworkNoAuth
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.ProxyURL = "https://user:private-value@proxy.example:8443"
	if p.Validate() == nil {
		t.Fatal("public proxy URL accepted a credential")
	}
	p.ProxyURL, p.ProxyAuth, p.RelayMode = "https://proxy.example:8443", NetworkBasicAuth, NetworkDocker
	if p.Validate() == nil {
		t.Fatal("model profile accepted a Docker-host relay")
	}
	p.RelayMode = NetworkLoopback
	if p.ValidateSaved() == nil {
		t.Fatal("authenticated profile had no credential revision")
	}
	p.HasCredentials, p.CredentialRevision = true, "credential_revision"
	if err := p.ValidateSaved(); err != nil {
		t.Fatal(err)
	}
	for _, credential := range []ProxyCredential{{Username: "", Password: "value"}, {Username: "a:b", Password: "value"}, {Username: "user", Password: "value\n"}} {
		if credential.Validate() == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}

func TestProfileTLSSkipRequiresRevisionRiskButPreservesIgnoredCA(t *testing.T) {
	p := networkFixture(NetworkEnvironment, "env", "execution")
	p.TLSMode, p.TrustMode, p.CACertPath = NetworkSkipTLS, NetworkCustomCA, "/does-not-exist/retained.pem"
	if p.Validate() == nil {
		t.Fatal("skip accepted without independent risk")
	}
	p.TLSRiskAccepted = true
	if err := p.ValidateSaved(); err != nil {
		t.Fatal(err)
	}
	if p.CACertPath != "/does-not-exist/retained.pem" {
		t.Fatal("ignored CA value was lost")
	}
	p.TLSMode, p.CACertPath = NetworkVerifyTLS, ""
	if p.Validate() == nil {
		t.Fatal("restored custom trust without a CA path")
	}
}

func TestNetworkMirrorSourcesAreExplicitAndCredentialFree(t *testing.T) {
	p := networkFixture(NetworkEnvironment, "env", "execution")
	p.Mirrors.APT = NetworkSource{Mode: NetworkCustom, Sources: "deb https://mirror.example/debian stable main\n"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, source := range []NetworkSource{
		{Mode: NetworkNative, URL: "https://old.example"},
		{Mode: NetworkCustom, Sources: "deb [trusted=yes] https://mirror.example/debian stable main"},
		{Mode: NetworkCustom, Sources: "deb https://user:secret@mirror.example/debian stable main"},
		{Mode: NetworkCustom, Sources: "deb https://mirror.example/debian"},
	} {
		p.Mirrors.APT = source
		if p.Validate() == nil {
			t.Fatal("ambiguous or insecure apt source accepted")
		}
	}
	p.Mirrors.APT = NetworkSource{Mode: NetworkNative}
	p.Mirrors.NPM = NetworkSource{Mode: NetworkCustom, URL: "https://user:secret@mirror.example/npm"}
	if p.Validate() == nil {
		t.Fatal("mirror URL credential accepted")
	}
}

func TestOriginalBuildAndVerifierChecksCannotBeRelaxed(t *testing.T) {
	for name, change := range map[string]func(*RunSnapshot){
		"build-tls": func(s *RunSnapshot) {
			s.Network.Build.ID = "net_build"
			s.Environment.Networks.BuildProfileID = "net_build"
			s.Network.Build.TLSMode = NetworkSkipTLS
			s.Network.Build.TLSRiskAccepted = true
		},
		"build-ca": func(s *RunSnapshot) {
			s.Network.Build.ID = "net_build"
			s.Environment.Networks.BuildProfileID = "net_build"
			s.Network.Build.TrustMode = NetworkCustomCA
			s.Network.Build.CACertPath = "/ca.pem"
		},
		"build-mirror": func(s *RunSnapshot) {
			s.Network.Build.ID = "net_build"
			s.Environment.Networks.BuildProfileID = "net_build"
			mirrors := *s.Network.Build.Mirrors
			mirrors.NPM = NetworkSource{Mode: NetworkCustom, URL: "https://mirror.example/npm"}
			s.Network.Build.Mirrors = &mirrors
		},
		"verifier-tls": func(s *RunSnapshot) {
			s.Network.Verifier.ID = "net_verifier"
			s.Environment.Networks.VerifierProfileID = "net_verifier"
			s.Network.Verifier.TLSMode = NetworkSkipTLS
			s.Network.Verifier.TLSRiskAccepted = true
		},
		"model-host": func(s *RunSnapshot) {
			s.Network.Model = networkFixture(NetworkEnvironment, "env", "model")
			s.Provider.NetworkProfileID = s.Network.Model.ID
		},
		"manual-gateway-without-address": func(s *RunSnapshot) {
			s.Environment.DockerGatewayMode = "manual"
			s.Environment.DockerHostGateway = " "
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := snapshotFixture(t)
			change(&s)
			s.ConfigHash, _ = s.ApprovalHash()
			if s.ValidateSources() == nil {
				t.Fatal("unsupported or unrelated stage setting accepted")
			}
		})
	}
}

func TestNetworkRevisionAndCredentialRevisionAreApprovalBound(t *testing.T) {
	for name, change := range map[string]func(*RunSnapshot){
		"profile":    func(s *RunSnapshot) { s.Network.Model.Revision = "new_revision" },
		"credential": func(s *RunSnapshot) { s.Network.Model.CredentialRevision = "new_credential_revision" },
		"destination": func(s *RunSnapshot) {
			s.Network.Model.ProxyMode = NetworkProxy
			s.Network.Model.ProxyURL = "https://proxy.example"
			s.Network.Model.ProxyTargets = []string{"https"}
			s.Network.Model.ProxyAuth = NetworkNoAuth
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := snapshotFixture(t)
			before := s.ConfigHash
			change(&s)
			after, err := s.ApprovalHash()
			if err != nil || before == after {
				t.Fatal("network boundary excluded from approval")
			}
			if s.ValidateSources() == nil {
				t.Fatal("mutated approved snapshot was accepted")
			}
		})
	}
	s := snapshotFixture(t)
	s.Network.Agent.ID, s.Environment.Networks.AgentProfileID = "net_agent", "net_agent"
	s.Network.Agent.TLSMode, s.Network.Agent.TLSRiskAccepted = NetworkSkipTLS, true
	s.ConfigHash, _ = s.ApprovalHash()
	if err := s.ValidateSources(); err != nil {
		t.Fatal(err)
	}
	if !s.Network.ExecutionTLSRisk() || s.Network.ModelTLSRisk() || s.Network.Verifier.TLSMode != NetworkVerifyTLS {
		t.Fatal("risk scopes were merged")
	}
}

func TestPrivateNetworkContextNeverAppearsInPublicEnvironmentSnapshot(t *testing.T) {
	s := snapshotFixture(t)
	private := &ExecutionNetworks{Snapshot: *s.Network, Credentials: map[string]ProxyCredential{"id@revision": {Username: "private-proxy-user", Password: "private-proxy-password"}}}
	s.Environment.NetworkRuntime = private
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "private-proxy") || strings.Contains(string(b), "network_runtime") {
		t.Fatal("private worker context leaked into public snapshot")
	}
	wire, err := json.Marshal(ExecutionSpec{Snapshot: s, Networks: private})
	if err != nil || !strings.Contains(string(wire), "private-proxy-password") {
		t.Fatal("private worker channel lost its owned network context")
	}
	old := s
	old.FormatVersion, old.Network = 2, nil
	b, _ = json.Marshal(old)
	var decoded RunSnapshot
	if json.Unmarshal(b, &decoded) != nil || decoded.Network != nil || decoded.FormatVersion != 2 || decoded.ValidateSources() == nil {
		t.Fatal("old record was fabricated or made startable")
	}
}

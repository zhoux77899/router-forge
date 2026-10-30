package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func selectedNativeSourceFixture(t *testing.T, environment domain.Environment) domain.Environment {
	t.Helper()
	var profile domain.NetworkProfile
	value := os.Getenv("ROUTER_FORGE_SOURCE_PROFILE_JSON")
	if value == "" || json.Unmarshal([]byte(value), &profile) != nil {
		t.Fatal("opt-in native checks require an explicit ROUTER_FORGE_SOURCE_PROFILE_JSON saved execution profile")
	}
	if err := profile.ValidateSaved(); err != nil || profile.EnvironmentID != environment.ID || profile.Scope != domain.NetworkEnvironment {
		t.Fatal("native fixture source profile must target its exact environment", err)
	}
	environment.WorkDir = projectTestDir(t)
	environment.Networks = &domain.EnvironmentNetworks{SourceProfileID: profile.ID}
	environment.NetworkRuntime = &domain.ExecutionNetworks{Snapshot: domain.NetworkSnapshot{Source: profile}, Credentials: map[string]domain.ProxyCredential{}}
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		var credential domain.ProxyCredential
		if json.Unmarshal([]byte(os.Getenv("ROUTER_FORGE_SOURCE_PROXY_CREDENTIAL_JSON")), &credential) != nil || credential.Validate() != nil {
			t.Fatal("native fixture requires its private proxy credential JSON")
		}
		environment.NetworkRuntime.Credentials[profile.ID+"@"+profile.CredentialRevision] = credential
	}
	environment.DockerGatewayMode = os.Getenv("ROUTER_FORGE_DOCKER_GATEWAY_MODE")
	environment.DockerHostGateway = os.Getenv("ROUTER_FORGE_DOCKER_GATEWAY")
	return environment
}

func authenticatedFixtureProfile(profile domain.NetworkProfile, upstream, relay string) domain.NetworkProfile {
	profile.ProxyMode = domain.NetworkProxy
	profile.ProxyURL = upstream
	profile.ProxyTargets = []string{"http", "https", "all"}
	profile.ProxyAuth = domain.NetworkBasicAuth
	profile.RelayMode = domain.NetworkRelayMode(relay)
	profile.HasCredentials = true
	profile.CredentialRevision = "credential_1"
	return profile
}

func TestExplicitNetworkRequiresSelectionAndFrozenPrivateAgreement(t *testing.T) {
	spec := fixtureSpec()
	if err := ValidateExecutionSpec(spec); err != nil {
		t.Fatal(err)
	}
	cases := []func(*domain.ExecutionSpec){
		func(s *domain.ExecutionSpec) { s.Networks = nil },
		func(s *domain.ExecutionSpec) { s.Networks.Snapshot.Agent.Revision = "new_revision" },
		func(s *domain.ExecutionSpec) { s.Snapshot.Environment.Networks = nil; sealFixtureSpec(s) },
		func(s *domain.ExecutionSpec) {
			s.Snapshot.Network.Verifier.TLSMode = domain.NetworkSkipTLS
			s.Snapshot.Network.Verifier.TLSRiskAccepted = true
			sealFixtureSpec(s)
		},
		func(s *domain.ExecutionSpec) {
			s.Snapshot.Network.Build.Mirrors.NPM = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://mirror.fixture.invalid/npm"}
			sealFixtureSpec(s)
		},
		func(s *domain.ExecutionSpec) {
			s.Networks.Credentials = map[string]domain.ProxyCredential{"desktop@credential": {Username: "private-name", Password: "private-password"}}
		},
	}
	for index, mutate := range cases {
		s := fixtureSpec()
		mutate(&s)
		if err := ValidateExecutionSpec(s); err == nil {
			t.Fatalf("missing/tampered explicit network accepted: %d", index)
		}
	}
	if _, _, err := sourceNetwork(domain.Environment{}); err == nil {
		t.Fatal("missing source selection inherited host networking")
	}
	env := spec.Snapshot.Environment
	env.NetworkRuntime = &domain.ExecutionNetworks{Snapshot: domain.NetworkSnapshot{Source: spec.Snapshot.Network.Source}}
	if _, _, err := sourceNetwork(env); err != nil {
		t.Fatal("source-only operation required unrelated stages", err)
	}
	env.NetworkRuntime.Snapshot.Source.EnvironmentID = "another_host"
	if _, _, err := sourceNetwork(env); err == nil {
		t.Fatal("another host's source profile accepted")
	}
}

func TestPrivateSourceCommandDropsUnrelatedStageCredentials(t *testing.T) {
	spec := fixtureSpec()
	credential := domain.ProxyCredential{Username: "private-proxy-user", Password: "private-proxy-password"}
	spec.Snapshot.Network.Source = authenticatedFixtureProfile(spec.Snapshot.Network.Source, "http://proxy.fixture.invalid:8080", "loopback")
	spec.Snapshot.Network.Agent = authenticatedFixtureProfile(spec.Snapshot.Network.Agent, "http://proxy.fixture.invalid:8080", "docker")
	sealFixtureSpec(&spec)
	spec.Networks.Credentials = map[string]domain.ProxyCredential{}
	for _, profile := range []domain.NetworkProfile{spec.Snapshot.Network.Source, spec.Snapshot.Network.Agent} {
		spec.Networks.Credentials[profile.ID+"@"+profile.CredentialRevision] = credential
	}
	env := spec.Snapshot.Environment
	env.NetworkRuntime = spec.Networks
	copied, err := privateCommandNetworks(env)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Snapshot.Agent.ID != "" || len(copied.Credentials) != 1 {
		t.Fatal("source operation transported agent/verifier credentials")
	}
	spec.Networks.Credentials[spec.Snapshot.Network.Source.ID+"@credential_1"] = domain.ProxyCredential{Username: "changed", Password: "changed"}
	if copied.Credentials[spec.Snapshot.Network.Source.ID+"@credential_1"] != credential {
		t.Fatal("queued operation retained mutable credential map")
	}
	data, _ := json.Marshal(env)
	if strings.Contains(string(data), credential.Password) || strings.Contains(string(data), "credentials") {
		t.Fatal("transient network appeared in public Environment")
	}
}

func TestPhaseNetworksHaveDistinctProxiesMirrorsAndStrictVerifier(t *testing.T) {
	spec := fixtureSpec()
	for _, profile := range []*domain.NetworkProfile{&spec.Snapshot.Network.Source, &spec.Snapshot.Network.Build, &spec.Snapshot.Network.Agent, &spec.Snapshot.Network.Verifier} {
		profile.ProxyMode = domain.NetworkProxy
		profile.ProxyAuth = domain.NetworkNoAuth
		profile.ProxyURL = "http://" + profile.Name + ".fixture.invalid:8080"
		profile.ProxyTargets = []string{"https"}
	}
	spec.Snapshot.Network.Agent.TLSMode = domain.NetworkSkipTLS
	spec.Snapshot.Network.Agent.TLSRiskAccepted = true
	spec.Snapshot.Network.Agent.TrustMode = domain.NetworkCustomCA
	spec.Snapshot.Network.Agent.CACertPath = "/missing/inactive.pem"
	spec.Snapshot.Network.Agent.Mirrors.APT = domain.NetworkSource{Mode: domain.NetworkCustom, Sources: "deb https://apt.fixture.invalid/repo explicit-suite main"}
	spec.Snapshot.Network.Agent.Mirrors.PyPI = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://pypi.fixture.invalid/simple"}
	spec.Snapshot.Network.Agent.Mirrors.NPM = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://npm.fixture.invalid/"}
	spec.Snapshot.Network.Agent.Mirrors.Node = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://node.fixture.invalid/dist"}
	spec.Snapshot.Network.Agent.Mirrors.UV = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://uv.fixture.invalid/simple"}
	spec.Snapshot.Network.Agent.Mirrors.UVPython = domain.NetworkSource{Mode: domain.NetworkCustom, URL: "https://python.fixture.invalid/releases"}
	sealFixtureSpec(&spec)
	before := spec.Snapshot.ConfigHash
	root := projectTestDir(t)
	network, err := prepareExecutionNetwork(context.Background(), spec, root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer network.close()
	if network.Agent.Env["HTTP_PROXY"] != "" || network.Agent.Env["HTTPS_PROXY"] != spec.Snapshot.Network.Agent.ProxyURL || network.Verifier.Env["HTTPS_PROXY"] != spec.Snapshot.Network.Verifier.ProxyURL || network.BuildArgs["HTTPS_PROXY"] != spec.Snapshot.Network.Build.ProxyURL {
		t.Fatal("proxy phase/target settings collapsed")
	}
	if network.Verifier.Env["ROUTER_FORGE_SKIP_TLS_VERIFICATION"] != "" || network.Verifier.Env["npm_config_strict_ssl"] != "true" || network.Verifier.Env["PYTHONPATH"] == network.Agent.Env["PYTHONPATH"] || network.Agent.Env["SSL_CERT_FILE"] != "" {
		t.Fatal("agent relaxation/CA shim reached verifier")
	}
	for key, value := range map[string]string{"PIP_INDEX_URL": spec.Snapshot.Network.Agent.Mirrors.PyPI.URL, "npm_config_registry": spec.Snapshot.Network.Agent.Mirrors.NPM.URL, "UV_DEFAULT_INDEX": spec.Snapshot.Network.Agent.Mirrors.UV.URL, "UV_PYTHON_INSTALL_MIRROR": spec.Snapshot.Network.Agent.Mirrors.UVPython.URL, "NODEJS_ORG_MIRROR": spec.Snapshot.Network.Agent.Mirrors.Node.URL} {
		if network.Agent.Env[key] != value || network.Verifier.Env[key] != "" {
			t.Fatalf("mirror not isolated: %s", key)
		}
	}
	apt, _ := os.ReadFile(filepath.Join(network.Agent.directory, "apt.list"))
	if strings.TrimSpace(string(apt)) != spec.Snapshot.Network.Agent.Mirrors.APT.Sources {
		t.Fatal("APT suite inferred or .list altered")
	}
	config, err := buildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", filepath.Join(root, "jobs"), filepath.Join(root, "compose.json"), network)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Environment.Env) != 0 || !reflect.DeepEqual(config.Verifier.Env, network.Verifier.Env) || !reflect.DeepEqual(config.Agents[0].Env, network.Agent.Env) || len(config.Environment.Mounts) != 2 || config.Environment.Kwargs["mounts"] == nil {
		t.Fatal("native schema lost phase boundaries")
	}
	if spec.Snapshot.ConfigHash != before || spec.Snapshot.ValidateSources() != nil {
		t.Fatal("preparing network changed approval")
	}
}

func TestAuthenticatedSourceRelayIsPrivateAndClosesWithOperation(t *testing.T) {
	credential := domain.ProxyCredential{Username: "owned-relay-user", Password: "owned-relay-password"}
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Proxy-Authorization")
		io.WriteString(w, "owned-proxy-result")
	}))
	defer upstream.Close()
	env := fixtureSpec().Snapshot.Environment
	env.WorkDir = projectTestDir(t)
	profile := authenticatedFixtureProfile(fixtureNetworkSnapshot(env.ID).Source, upstream.URL, "loopback")
	env.NetworkRuntime = &domain.ExecutionNetworks{Snapshot: domain.NetworkSnapshot{Source: profile}, Credentials: map[string]domain.ProxyCredential{profile.ID + "@credential_1": credential}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, stage, processEnv, closeNetwork, err := prepareSourceOperation(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	defer closeNetwork()
	endpoint, _ := url.Parse(stage.relay.URL)
	if !net.ParseIP(endpoint.Hostname()).Equal(net.ParseIP("127.0.0.1")) || endpoint.User != nil {
		t.Fatal("source relay is not credential-free exact loopback")
	}
	transport := &http.Transport{Proxy: http.ProxyURL(endpoint)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("http://selected-source.fixture.invalid/task")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if <-seen != "Basic "+base64.StdEncoding.EncodeToString([]byte(credential.Username+":"+credential.Password)) {
		t.Fatal("private credential was not injected only upstream")
	}
	public, _ := json.Marshal(stage)
	joined := string(public) + strings.Join(processEnv, "\n")
	filepath.WalkDir(stage.directory, func(filename string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			data, _ := os.ReadFile(filename)
			joined += string(data)
		}
		return err
	})
	if strings.Contains(joined, credential.Username) || strings.Contains(joined, credential.Password) || strings.Contains(joined, "Basic ") {
		t.Fatal("credential leaked to configuration/env")
	}
	apt, _ := os.ReadFile(filepath.Join(stage.directory, "apt.conf"))
	if !strings.Contains(string(apt), stage.relay.URL) {
		t.Fatal("APT bypassed the owned authentication relay")
	}
	directory := stage.directory
	cancel()
	select {
	case <-stage.relay.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled relay did not close")
	}
	closeNetwork()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("operation config was retained")
	}
	if _, err := client.Get("http://selected-source.fixture.invalid/task"); err == nil {
		t.Fatal("closed relay still accepted work")
	}
}

func TestTargetHostNetworkCheckStrictSkipAndCustomCARestore(t *testing.T) {
	server := newUntrustedTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "owned-check") }))
	defer server.Close()
	env := fixtureSpec().Snapshot.Environment
	env.WorkDir = projectTestDir(t)
	ca := filepath.Join(env.WorkDir, "owned-ca.pem")
	if err := writePrivate(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); err != nil {
		t.Fatal(err)
	}
	base := fixtureNetworkSnapshot(env.ID).Source
	for _, test := range []struct {
		name                    string
		skip, custom, reachable bool
	}{{"strict", false, false, false}, {"skip-inactive-ca", true, true, true}, {"custom-restore", false, true, true}, {"strict-restore", false, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			if test.custom {
				profile.TrustMode = domain.NetworkCustomCA
				profile.CACertPath = ca
			}
			if test.skip {
				profile.TLSMode = domain.NetworkSkipTLS
				profile.TLSRiskAccepted = true
				profile.CACertPath = filepath.Join(env.WorkDir, "missing-inactive.pem")
			}
			result, err := (&nativeWorker{}).CheckNetwork(context.Background(), env, domain.NetworkOperation{Profile: profile, TargetURL: server.URL})
			if err != nil || result.Reachable != test.reachable {
				t.Fatalf("actual scoped check: %+v %v", result, err)
			}
			if result.Reachable && (result.TLSVerified == nil || *result.TLSVerified == test.skip) {
				t.Fatal("TLS observation misrepresented actual verification")
			}
			if test.custom && !test.skip && result.CASHA256 == "" {
				t.Fatal("custom CA hash missing")
			}
			if test.skip && result.CASHA256 != "" {
				t.Fatal("inactive CA read")
			}
		})
	}
	if _, err := (&nativeWorker{}).CheckNetwork(context.Background(), env, domain.NetworkOperation{Profile: base, TargetURL: ""}); err == nil {
		t.Fatal("implicit probe URL accepted")
	}
}

func TestTargetHostNetworkCheckUsesHEADWithProxyOnlyCredentials(t *testing.T) {
	type requestObservation struct {
		method               string
		authorizationPresent bool
		proxyAuthPresent     bool
	}
	originRequests := make(chan requestObservation, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests <- requestObservation{
			method:               r.Method,
			authorizationPresent: len(r.Header.Values("Authorization")) != 0,
			proxyAuthPresent:     len(r.Header.Values("Proxy-Authorization")) != 0,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	credential := domain.ProxyCredential{Username: "owned-check-proxy-user", Password: "owned-check-proxy-password"}
	proxyRequests := make(chan requestObservation, 1)
	proxyAuthenticated := make(chan bool, 1)
	forwardTransport := &http.Transport{Proxy: nil}
	defer forwardTransport.CloseIdleConnections()
	forwardClient := &http.Client{Transport: forwardTransport, Timeout: 3 * time.Second}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- requestObservation{method: r.Method, authorizationPresent: len(r.Header.Values("Authorization")) != 0}
		proxyAuthenticated <- r.Header.Get("Proxy-Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte(credential.Username+":"+credential.Password))
		forward := r.Clone(r.Context())
		forward.URL.Scheme, forward.URL.Host = originURL.Scheme, originURL.Host
		forward.RequestURI = ""
		forward.Header.Del("Proxy-Authorization")
		response, err := forwardClient.Do(forward)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	env := fixtureSpec().Snapshot.Environment
	env.WorkDir = projectTestDir(t)
	profile := authenticatedFixtureProfile(fixtureNetworkSnapshot(env.ID).Source, proxy.URL, "loopback")
	profile.NoProxy = ""
	result, err := (&nativeWorker{}).CheckNetwork(context.Background(), env, domain.NetworkOperation{
		Profile: profile, Credential: &credential, TargetURL: "http://selected-check.fixture.invalid/owned-probe",
	})
	if err != nil || !result.Reachable || result.HTTPStatus == nil || *result.HTTPStatus != http.StatusNoContent {
		t.Fatalf("owned target check did not succeed: %+v %v", result, err)
	}
	if !<-proxyAuthenticated {
		t.Fatal("selected private credential was not used exclusively for upstream proxy authentication")
	}
	for _, observation := range []requestObservation{<-proxyRequests, <-originRequests} {
		if observation.authorizationPresent || observation.proxyAuthPresent {
			t.Fatal("network check sent an origin authorization header")
		}
		if observation.method != http.MethodHead {
			t.Fatalf("network check method = %s, want HEAD", observation.method)
		}
	}
}

func TestNetworkEvidenceAndErrorsNeverContainPrivateCredentials(t *testing.T) {
	spec := fixtureSpec()
	secret := domain.ProxyCredential{Username: "secret-profile-user", Password: "secret-profile-password"}
	spec.Snapshot.Network.Source = authenticatedFixtureProfile(spec.Snapshot.Network.Source, "http://proxy.fixture.invalid:8080", "loopback")
	sealFixtureSpec(&spec)
	spec.Networks.Credentials = map[string]domain.ProxyCredential{spec.Snapshot.Network.Source.ID + "@credential_1": secret}
	path := filepath.Join(projectTestDir(t), "provenance.json")
	record := map[string]any{"skip_tls_verification": true, "tls_verification_scope": "obsolete"}
	setNetworkProvenance(record, spec.Snapshot)
	data, _ := json.Marshal(record)
	if err := writePrivate(path, data); err != nil {
		t.Fatal(err)
	}
	if err := finalizeProvenance(path, record, spec.Snapshot, domain.ExecutionResult{State: "infrastructure_error"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), secret.Username) || strings.Contains(string(data), secret.Password) || strings.Contains(string(data), "skip_tls_verification") || !strings.Contains(string(data), "frozen_configuration_not_connection_success") || !strings.Contains(string(data), "credential_1") {
		t.Fatal("false/private failure evidence", string(data))
	}
	raw := strings.Repeat("raw-version-line ", 100) + secret.Username + " " + secret.Password + " Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(secret.Username+":"+secret.Password))
	redacted := redactCommandText(raw, workerCommand{Spec: &spec})
	if len(redacted) < 512 || strings.Contains(redacted, secret.Username) || strings.Contains(redacted, secret.Password) {
		t.Fatal("raw bounded version observation truncated or leaked secret")
	}
	info := dockerNetworkInfo{HTTPProxy: "http://user:password@proxy.fixture.invalid:8080?token=private", HTTPSProxy: "https://user:password@proxy.fixture.invalid:8443", NoProxy: "secret=user@host"}
	info.RegistryConfig.Mirrors = []string{"https://user:password@mirror.fixture.invalid/?token=private"}
	encoded, _ := json.Marshal(observedDockerNetwork(info))
	for _, bad := range []string{"password", "token=", "user@", "secret=user"} {
		if strings.Contains(string(encoded), bad) {
			t.Fatalf("Docker observation leaked %s", bad)
		}
	}
}

func TestDockerProxyAuthorizationRequiresCurrentOwnedEndpointAndReadonlyMount(t *testing.T) {
	spec := fixtureSpec()
	root := projectTestDir(t)
	stageDir := filepath.Join(root, "network", "agent")
	if err := privateDir(stageDir); err != nil {
		t.Fatal(err)
	}
	tracker := newDockerTracker("fixture-docker", nil, "owner_fixture", root, filepath.Join(root, "jobs"), spec)
	owned := DockerContainer{ID: strings.Repeat("a", 64), Running: true, Labels: map[string]string{"io.router-forge.owner": "owner_fixture", "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}, Mounts: []dockerNetworkMount{{Source: stageDir, Destination: containerNetworkRoot + "/agent", RW: false}}, Networks: map[string]dockerEndpoint{"private": {IPAddress: "172.22.0.8"}}}
	current := owned
	tracker.runner = func(ctx context.Context, c Command, env []string, dir string, input io.Reader, limit int) ([]byte, int, error) {
		switch c.Args[0] {
		case "ps":
			return []byte(owned.ID + "\n"), 0, nil
		case "inspect":
			data, _ := json.Marshal(current)
			return data, 0, nil
		}
		return nil, 0, nil
	}
	allowed := func() bool {
		return tracker.authorizeNetworkPeer(context.Background(), net.ParseIP("172.22.0.8"), "agent", stageDir)
	}
	if !allowed() {
		t.Fatal("exact owned mounted endpoint denied")
	}
	current.Networks = map[string]dockerEndpoint{"private": {IPAddress: "172.22.0.9"}}
	if allowed() {
		t.Fatal("stale peer IP authorized")
	}
	current = owned
	current.Mounts = []dockerNetworkMount{{Source: stageDir, Destination: containerNetworkRoot + "/agent", RW: true}}
	if allowed() {
		t.Fatal("writable/unproven mount authorized")
	}
	current = owned
	current.Labels = map[string]string{"io.router-forge.owner": "foreign", "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}
	if allowed() {
		t.Fatal("foreign project authorized")
	}
	current = owned
	if tracker.authorizeNetworkPeer(context.Background(), net.ParseIP("172.22.0.7"), "agent", stageDir) || tracker.authorizeNetworkPeer(context.Background(), net.ParseIP("127.0.0.1"), "agent", stageDir) {
		t.Fatal("subnet/NAT/loopback fallback authorized")
	}
}

func TestDockerGatewaySelectionAndReadOnlyObservation(t *testing.T) {
	var calls []Command
	runner := func(ctx context.Context, command Command, env []string, dir string, input io.Reader, limit int) ([]byte, int, error) {
		calls = append(calls, command)
		if command.Args[0] == "info" {
			return []byte(`{"OSType":"linux","HTTPProxy":"http://private-user:private-password@proxy.fixture.invalid:8080?token=private","RegistryConfig":{"Mirrors":["https://user:password@mirror.fixture.invalid/?token=private"]}}`), 0, nil
		}
		if len(command.Args) > 2 && command.Args[0] == "network" && command.Args[1] == "inspect" {
			return []byte(`[{"Gateway":"172.22.0.1"}]`), 0, nil
		}
		t.Fatal("Docker observation attempted a mutation", command.Args)
		return nil, 0, nil
	}
	validate := func(ip string) error {
		if ip == "172.22.0.1" || ip == "172.23.0.1" {
			return nil
		}
		return errors.New("fixture address is not an owned private bind")
	}
	for _, value := range []struct {
		mode, requested, expected string
		commands                  int
	}{{"detect", "172.23.0.1", "172.22.0.1", 2}, {"manual", "172.23.0.1", "172.23.0.1", 1}} {
		calls = nil
		gateway, observation, err := inspectDockerNetworkWith(context.Background(), "fixture-docker", value.mode, value.requested, runner, validate)
		if err != nil || gateway != value.expected || len(calls) != value.commands {
			t.Fatal("selection inferred or ignored an address", gateway, err)
		}
		data, _ := json.Marshal(observation)
		if observation.ObservedAt == "" || strings.Contains(string(data), "private-user") || strings.Contains(string(data), "password") || strings.Contains(string(data), "token=") {
			t.Fatal("daemon credentials reached a public observation")
		}
	}
	for _, value := range []struct{ mode, requested string }{{"", ""}, {"manual", ""}, {"manual", "0.0.0.0"}} {
		if _, _, err := inspectDockerNetworkWith(context.Background(), "fixture-docker", value.mode, value.requested, runner, validate); err == nil {
			t.Fatal("missing/unowned selection accepted")
		}
	}
}

func TestContainerProxyAuthenticationRequiresExactCapabilityAndCleansFailure(t *testing.T) {
	spec := fixtureSpec()
	credential := domain.ProxyCredential{Username: "container-private-user", Password: "container-private-password"}
	profile := authenticatedFixtureProfile(spec.Snapshot.Network.Agent, "http://proxy.fixture.invalid:8080", "docker")
	for _, gateway := range []string{"", "0.0.0.0", "127.0.0.1", "192.0.2.1"} {
		root := projectTestDir(t)
		if _, err := prepareStageNetwork(context.Background(), profile, &credential, "agent", root, gateway, nil); err == nil {
			t.Fatal("unproven container relay was exposed")
		}
		if _, err := os.Stat(stageDirectory(root, "agent", false)); !os.IsNotExist(err) {
			t.Fatal("failed private stage configuration was retained")
		}
	}
}

func TestDockerProxyAuthorizationRejectsAmbiguityAndAllowsOwnedSeparateVerifier(t *testing.T) {
	spec := fixtureSpec()
	root := projectTestDir(t)
	jobDir := filepath.Join(root, "jobs")
	tracker := newDockerTracker("fixture-docker", nil, "owner_fixture", root, jobDir, spec)
	trial := tracker.trialPrefix + "__ownedtrial"
	workingDir := filepath.Join(jobDir, trial)
	if err := privateDir(workingDir); err != nil {
		t.Fatal(err)
	}
	stageDir := stageDirectory(root, "verifier", false)
	if err := privateDir(stageDir); err != nil {
		t.Fatal(err)
	}
	project := strings.ToLower(trial) + "__verifier__fixture"
	verifier := DockerContainer{ID: strings.Repeat("b", 64), Running: true, Labels: map[string]string{"com.docker.compose.project": project, "com.docker.compose.project.working_dir": workingDir}, Mounts: []dockerNetworkMount{{Source: stageDir, Destination: containerNetworkRoot + "/verifier"}}, Networks: map[string]dockerEndpoint{"owned": {IPAddress: "172.22.0.12"}}}
	containers := map[string]DockerContainer{verifier.ID: verifier}
	tracker.runner = func(ctx context.Context, command Command, env []string, dir string, input io.Reader, limit int) ([]byte, int, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if command.Args[0] == "inspect" {
			data, _ := json.Marshal(containers[command.Args[1]])
			return data, 0, nil
		}
		if command.Args[0] == "ps" {
			if strings.Contains(strings.Join(command.Args, " "), "--format") {
				return []byte(project), 0, nil
			}
			filter := command.Args[len(command.Args)-1]
			if strings.Contains(filter, "io.router-forge.owner") {
				return nil, 0, nil
			}
			var ids []string
			for id := range containers {
				ids = append(ids, id)
			}
			return []byte(strings.Join(ids, "\n")), 0, nil
		}
		return nil, 0, errors.New("unexpected fixture Docker command")
	}
	peer := net.ParseIP("172.22.0.12")
	if !tracker.authorizeNetworkPeer(context.Background(), peer, "verifier", stageDir) {
		t.Fatal("actual private native verifier family and readonly endpoint denied")
	}
	duplicate := verifier
	duplicate.ID = strings.Repeat("c", 64)
	containers[duplicate.ID] = duplicate
	if tracker.authorizeNetworkPeer(context.Background(), peer, "verifier", stageDir) {
		t.Fatal("ambiguous shared/NAT endpoint granted relay access")
	}
	delete(containers, duplicate.ID)
	verifier.Labels = map[string]string{"com.docker.compose.project": "foreign__verifier__fixture", "com.docker.compose.project.working_dir": workingDir}
	containers[verifier.ID] = verifier
	if tracker.authorizeNetworkPeer(context.Background(), peer, "verifier", stageDir) {
		t.Fatal("foreign native verifier family granted access")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if tracker.authorizeNetworkPeer(ctx, peer, "verifier", stageDir) {
		t.Fatal("cancelled run retained authorization")
	}
}

func TestPrivateNetworkObservationsRedactBeforeLocalAndDurableSnapshots(t *testing.T) {
	spec := fixtureSpec()
	secret := domain.ProxyCredential{Username: "private-observation-user", Password: "private-observation-password"}
	spec.Snapshot.Network.Source = authenticatedFixtureProfile(spec.Snapshot.Network.Source, "http://proxy.fixture.invalid:8080", "loopback")
	sealFixtureSpec(&spec)
	spec.Networks.Credentials = map[string]domain.ProxyCredential{spec.Snapshot.Network.Source.ID + "@credential_1": secret}
	observations := fixtureObservations(spec, domain.Environment{WorkDir: projectTestDir(t)})
	observations[0].Tools[0].VersionOutput = strings.Repeat("retained actual version line\n", 100) + secret.Username + " " + secret.Password
	observations[2].AgentVersionOutput += " " + secret.Username + " " + secret.Password
	for _, value := range observations {
		if err := recordPrivateObservation(&spec, func(observed domain.ExecutionObservation) error {
			data, _ := json.Marshal(observed)
			if strings.Contains(string(data), secret.Username) || strings.Contains(string(data), secret.Password) {
				t.Fatal("private credential reached the durable sink")
			}
			return nil
		}, value); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := json.Marshal(spec.Snapshot)
	if strings.Contains(string(data), secret.Username) || strings.Contains(string(data), secret.Password) {
		t.Fatal("private credential reached the local provenance snapshot")
	}
	if len(spec.Snapshot.Runtime.Tools[0].VersionOutput) < 1024 {
		t.Fatal("redaction truncated bounded raw version evidence")
	}
}

func TestPrivateNetworkInspectionAndResultAreRedactedAtParentBoundary(t *testing.T) {
	spec := fixtureSpec()
	secret := domain.ProxyCredential{Username: "private-result-user", Password: "private-result-password"}
	spec.Snapshot.Network.Source = authenticatedFixtureProfile(spec.Snapshot.Network.Source, "http://proxy.fixture.invalid:8080", "loopback")
	sealFixtureSpec(&spec)
	spec.Networks.Credentials = map[string]domain.ProxyCredential{spec.Snapshot.Network.Source.ID + "@credential_1": secret}
	command := workerCommand{Spec: &spec}
	pending := &pendingCommand{reply: make(chan commandReply, 1), redact: func(value string) string { return redactCommandText(value, command) }}
	session := &workerSession{pending: map[string]*pendingCommand{"private-result-fixture": pending}}
	value := commandReply{ID: "private-result-fixture", Inspection: &domain.Inspection{Ready: true, Tools: []domain.ToolStatus{{Name: "harbor", VersionOutput: strings.Repeat("actual version evidence ", 100) + secret.Password}}, Agents: []domain.Agent{{Name: "opencode", Reason: secret.Username, Schema: json.RawMessage(`{"description":"` + secret.Password + `"}`)}}, Issues: []string{secret.Password}}, Result: &domain.ExecutionResult{AgentVersion: secret.Username, Error: secret.Password}}
	body, _ := json.Marshal(value)
	r := httptest.NewRequest(http.MethodPost, "/results", strings.NewReader(string(body)))
	r.Header.Set("X-Router-Forge-Version", domain.Version)
	response := httptest.NewRecorder()
	session.ServeHTTP(response, r)
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code)
	}
	received := <-pending.reply
	data, _ := json.Marshal(received)
	if strings.Contains(string(data), secret.Username) || strings.Contains(string(data), secret.Password) {
		t.Fatal("untrusted worker output leaked a private credential to the parent")
	}
	if len(received.Inspection.Tools[0].VersionOutput) < 1024 {
		t.Fatal("raw version evidence was truncated")
	}
}

func TestOfflinePythonPhaseContracts(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("installed Python unavailable for stdlib-only phase tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data, err := runTLSClientFixture(ctx, Command{Path: python, Args: []string{"-I", "-B", "harbor_network_test.py"}}, transportEnvironment())
	if err != nil {
		t.Fatalf("offline Python phase tests failed: %v\n%s", err, data)
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	backend "github.com/zhoux77899/router-forge/internal/runtime"
)

// This opt-in test exercises production resources and Go bindings without opening a GUI.
func TestBundledConsoleIntegration(t *testing.T) {
	if os.Getenv("ROUTER_FORGE_VERIFY_NATIVE_BUNDLE") != "1" {
		t.Skip("set ROUTER_FORGE_VERIFY_NATIVE_BUNDLE=1 and ROUTER_FORGE_NATIVE_BUNDLE_DIR after building Desktop")
	}
	resources := os.Getenv("ROUTER_FORGE_NATIVE_BUNDLE_DIR")
	if !filepath.IsAbs(resources) {
		t.Fatal("ROUTER_FORGE_NATIVE_BUNDLE_DIR must be an absolute bundle directory")
	}
	profile := t.TempDir()
	options := resourceOptions(resources, profile)
	nativeWorker := "router-forge-worker-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		nativeWorker += ".exe"
	}
	for _, path := range []string{options.ServerPath, filepath.Join(options.WebDir, "index.html"), filepath.Join(options.WorkerDir, nativeWorker), filepath.Join(options.WorkerDir, "router-forge-worker-linux-amd64"), filepath.Join(options.WorkerDir, "router-forge-worker-linux-arm64")} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("required production resource is missing")
		}
	}
	registerBundleBackendCleanup(t, profile)

	first := newConsole()
	first.options = func() (backend.Options, error) { return options, nil }
	var startupContext context.Context
	first.ensure = func(ctx context.Context, opts backend.Options) (domain.BackendInfo, error) {
		startupContext = ctx
		return backend.EnsureRunning(ctx, opts)
	}
	t.Cleanup(first.client.CloseIdleConnections)
	connection, err := first.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if connection.PID < 1 || connection.DataDir != profile || connection.WorkerDir != options.WorkerDir || connection.Version != domain.Version {
		t.Fatal("production connection metadata does not match the isolated bundle")
	}
	select {
	case <-startupContext.Done():
	default:
		t.Fatal("Connect retained its startup request context")
	}
	info, err := backend.ReadInfo(profile)
	if err != nil || !backend.Healthy(context.Background(), info) {
		t.Fatal("backend did not outlive the startup context")
	}
	encoded, _ := json.Marshal(connection)
	if strings.Contains(string(encoded), info.ControlToken) || strings.Contains(string(encoded), "control_token") {
		t.Fatal("renderer connection exposed the management credential")
	}

	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	key := "fixture-only-native-bundle-key"
	var initialProfiles []domain.NetworkProfile
	bundleAPICall(t, first, http.MethodGet, "/api/network-profiles", nil, &initialProfiles)
	if len(initialProfiles) != 0 {
		t.Fatal("fresh native storage inferred a network profile")
	}
	var savedEnvironment domain.Environment
	bundleAPICall(t, first, http.MethodPost, "/api/environments", domain.Environment{
		Name: "Offline bundle fixture", Type: "local", WorkDir: filepath.Join(profile, "executor"),
	}, &savedEnvironment)
	var savedProvider domain.Provider
	bundleAPICall(t, first, http.MethodPost, "/api/providers", domain.ProviderInput{
		Name: "Offline bundle fixture", BaseURL: provider.URL + "/v1", APIKey: &key,
	}, &savedProvider)
	if !savedProvider.HasKey || savedProvider.KeyHint == "" || savedProvider.KeyRevision == "" {
		t.Fatal("write-only fixture credential metadata was not persisted")
	}
	var savedModel domain.Model
	bundleAPICall(t, first, http.MethodPost, "/api/models", domain.Model{
		ProviderID: savedProvider.ID, Name: "fixture-model", DisplayName: "Offline fixture", Protocol: domain.ChatCompletions,
	}, &savedModel)

	var executionProfile domain.NetworkProfile
	bundleAPICall(t, first, http.MethodPost, "/api/network-profiles", domain.NetworkProfileInput{Profile: domain.NetworkProfile{
		Name: "Explicit offline host network", Scope: domain.NetworkEnvironment, EnvironmentID: savedEnvironment.ID,
		ProxyMode: domain.NetworkDirect, TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA,
		Mirrors: &domain.NetworkMirrors{
			APT: domain.NetworkSource{Mode: domain.NetworkNative}, PyPI: domain.NetworkSource{Mode: domain.NetworkNative},
			NPM: domain.NetworkSource{Mode: domain.NetworkNative}, Node: domain.NetworkSource{Mode: domain.NetworkNative},
			UV: domain.NetworkSource{Mode: domain.NetworkNative}, UVPython: domain.NetworkSource{Mode: domain.NetworkNative},
		},
	}}, &executionProfile)
	proxyUsername, proxyPassword := "fixture-only-native-bundle-proxy-user", "fixture-only-native-bundle-proxy-password"
	var modelProfile domain.NetworkProfile
	bundleAPICall(t, first, http.MethodPost, "/api/network-profiles", domain.NetworkProfileInput{
		Profile: domain.NetworkProfile{Name: "Explicit offline model network", Scope: domain.NetworkDesktop,
			ProxyMode: domain.NetworkProxy, ProxyURL: "http://127.0.0.1:9", ProxyTargets: []string{"https"},
			ProxyAuth: domain.NetworkBasicAuth, RelayMode: domain.NetworkLoopback,
			TLSMode: domain.NetworkVerifyTLS, TrustMode: domain.NetworkSystemCA},
		ProxyUsername: &proxyUsername, ProxyPassword: &proxyPassword,
	}, &modelProfile)
	if !modelProfile.HasCredentials || modelProfile.CredentialRevision == "" || executionProfile.Revision == "" {
		t.Fatal("native network revisions or write-only credential metadata were not persisted")
	}
	savedEnvironment.DockerGatewayMode = "detect"
	savedEnvironment.Networks = &domain.EnvironmentNetworks{SourceProfileID: executionProfile.ID, BuildProfileID: executionProfile.ID, AgentProfileID: executionProfile.ID, VerifierProfileID: executionProfile.ID}
	bundleAPICall(t, first, http.MethodPost, "/api/environments", savedEnvironment, &savedEnvironment)
	bundleAPICall(t, first, http.MethodPost, "/api/providers", domain.ProviderInput{
		ID: savedProvider.ID, Name: savedProvider.Name, BaseURL: savedProvider.BaseURL, NetworkProfileID: &modelProfile.ID,
	}, &savedProvider)

	second := newConsole()
	second.options = func() (backend.Options, error) { return options, nil }
	t.Cleanup(second.client.CloseIdleConnections)
	reattached, err := second.Connect()
	if err != nil || reattached.PID != connection.PID || reattached.URL != connection.URL {
		t.Fatal("a second native bridge did not reattach to the same backend")
	}
	var environments []domain.Environment
	bundleAPICall(t, second, http.MethodGet, "/api/environments", nil, &environments)
	var providers []domain.Provider
	bundleAPICall(t, second, http.MethodGet, "/api/providers", nil, &providers)
	var models []domain.Model
	bundleAPICall(t, second, http.MethodGet, "/api/models", nil, &models)
	if len(environments) != 1 || environments[0].ID != savedEnvironment.ID || len(providers) != 1 || providers[0].ID != savedProvider.ID || len(models) != 1 || models[0].ID != savedModel.ID {
		t.Fatal("fixture configuration did not survive native bridge reattachment")
	}
	var profiles []domain.NetworkProfile
	bundleAPICall(t, second, http.MethodGet, "/api/network-profiles", nil, &profiles)
	if len(profiles) != 2 || environments[0].Networks == nil || *environments[0].Networks != *savedEnvironment.Networks || environments[0].DockerGatewayMode != "detect" || providers[0].NetworkProfileID != modelProfile.ID {
		t.Fatal("explicit network profiles or host/model bindings did not survive native reattachment")
	}
	verifyBundledNetworkChecks(t, second, executionProfile)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := first.Request(ctx, http.MethodGet, "/api/status", ""); err == nil {
		t.Fatal("cancelled bridge request was not cancelled")
	}
	if !backend.Healthy(context.Background(), info) {
		t.Fatal("cancelling a renderer request terminated the independent backend")
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Get(connection.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("public Web client could access provider configuration")
	}
	page, err := client.Get(connection.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(page.Header.Get("Content-Type"), "text/html") || !strings.Contains(page.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("bundled results Web or its headers are unavailable")
	}
	var html strings.Builder
	if _, err := io.Copy(&html, io.LimitReader(page.Body, (1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if html.Len() > 1<<20 {
		t.Fatal("bundled Web HTML is oversized")
	}
	match := regexp.MustCompile(`src="([^"]+\.js)"`).FindStringSubmatch(html.String())
	if len(match) != 2 {
		t.Fatal("bundled results Web did not reference JavaScript")
	}
	base, _ := url.Parse(connection.URL + "/")
	asset, err := url.Parse(match[1])
	if err != nil {
		t.Fatal(err)
	}
	asset = base.ResolveReference(asset)
	if asset.Scheme != base.Scheme || asset.Host != base.Host || !strings.HasPrefix(asset.Path, "/assets/") {
		t.Fatal("bundled Web asset escaped its local origin")
	}
	javascript, err := client.Get(asset.String())
	if err != nil {
		t.Fatal(err)
	}
	javascript.Body.Close()
	if javascript.StatusCode != http.StatusOK {
		t.Fatal("bundled results JavaScript is missing")
	}
	if providerCalls.Load() != 0 {
		t.Fatal("configuration-only bundle acceptance unexpectedly called a provider")
	}
}

func verifyBundledNetworkChecks(t *testing.T, console *Console, profile domain.NetworkProfile) {
	t.Helper()
	var requests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodHead || r.Header.Get("Authorization") != "" {
			t.Errorf("native host check method=%s authorization_present=%t; expected HEAD without inference credentials", r.Method, r.Header.Get("Authorization") != "")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	check := func(risk bool) domain.NetworkCheck {
		var result domain.NetworkCheck
		bundleAPICall(t, console, http.MethodPost, "/api/network-profiles/"+profile.ID+"/check", domain.NetworkCheckInput{
			ProfileRevision: profile.Revision, TargetURL: target.URL, ConfirmTLSRisk: risk,
		}, &result)
		return result
	}
	strict := check(false)
	if strict.Reachable || len(strict.Issues) == 0 || requests.Load() != 0 {
		t.Fatal("native worker accepted an untrusted server under strict system trust")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})
	caPath := filepath.Join(t.TempDir(), "selected-worker-ca.pem")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	profile.ID, profile.Revision, profile.CreatedAt = "", "", ""
	profile.Name, profile.TrustMode, profile.CACertPath = "Explicit native worker CA", domain.NetworkCustomCA, caPath
	bundleAPICall(t, console, http.MethodPost, "/api/network-profiles", domain.NetworkProfileInput{Profile: profile}, &profile)
	custom := check(false)
	digest := sha256.Sum256(ca)
	if !custom.Reachable || custom.TLSVerified == nil || !*custom.TLSVerified || custom.CASHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("native worker did not apply and identify the selected custom CA", custom)
	}
	profile.TLSMode, profile.CACertPath = domain.NetworkSkipTLS, filepath.Join(t.TempDir(), "missing-retained-ca.pem")
	bundleAPICall(t, console, http.MethodPost, "/api/network-profiles", domain.NetworkProfileInput{Profile: profile, ConfirmTLSRisk: true}, &profile)
	input, _ := json.Marshal(domain.NetworkCheckInput{ProfileRevision: profile.Revision, TargetURL: target.URL})
	response, err := console.Request(context.Background(), http.MethodPost, "/api/network-profiles/"+profile.ID+"/check", string(input))
	if err != nil || response.Status != http.StatusBadRequest || requests.Load() != 1 {
		t.Fatal("saving native TLS risk authorized a distinct check operation")
	}
	skipped := check(true)
	if !skipped.Reachable || skipped.TLSVerified == nil || *skipped.TLSVerified || skipped.CASHA256 != "" {
		t.Fatal("native worker read an ignored CA or did not honor independently confirmed TLS skip")
	}
	profile.TLSMode, profile.CACertPath = domain.NetworkVerifyTLS, caPath
	bundleAPICall(t, console, http.MethodPost, "/api/network-profiles", domain.NetworkProfileInput{Profile: profile}, &profile)
	restored := check(false)
	if !restored.Reachable || restored.TLSVerified == nil || !*restored.TLSVerified || profile.TLSRiskAccepted || restored.CASHA256 != custom.CASHA256 || requests.Load() != 3 {
		t.Fatal("native worker failed to restore strict custom trust")
	}
}

func bundleAPICall(t *testing.T, console *Console, method, path string, input, output any) {
	t.Helper()
	body := ""
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = string(encoded)
	}
	response, err := console.Request(context.Background(), method, path, body)
	if err != nil || response.Status != http.StatusOK {
		diagnostic := strings.ReplaceAll(response.Body, "fixture-only-harbor-api-key", "<REDACTED>")
		if len(diagnostic) > 2048 {
			diagnostic = diagnostic[:2048]
		}
		t.Fatalf("bundled bridge %s %s: status %d, error %v, diagnostic %s", method, path, response.Status, err, diagnostic)
	}
	for _, secret := range []string{console.info.ControlToken, "fixture-only-native-bundle-key", "fixture-only-native-bundle-proxy-user", "fixture-only-native-bundle-proxy-password"} {
		if strings.Contains(response.Body, secret) {
			t.Fatal("bundled bridge exposed a private credential")
		}
	}
	if err := json.Unmarshal([]byte(response.Body), output); err != nil {
		t.Fatal(err)
	}
}

func registerBundleBackendCleanup(t *testing.T, profile string) {
	t.Helper()
	t.Cleanup(func() {
		info, err := backend.ReadInfo(profile)
		if err != nil || !backend.Healthy(context.Background(), info) {
			return
		}
		process, err := os.FindProcess(info.PID)
		if err != nil {
			t.Errorf("locate owned test backend: %v", err)
			return
		}
		defer process.Release()
		if err := process.Kill(); err != nil {
			t.Errorf("stop owned test backend: %v", err)
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for backend.Healthy(context.Background(), info) {
			if time.Now().After(deadline) {
				t.Error("owned test backend did not stop")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

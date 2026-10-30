package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	backend "github.com/zhoux77899/router-forge/internal/runtime"
)

func TestCLIDataDirectoryOverridesEnvironment(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(cwd, ".mygo", "environment-profile")
	cliPath := filepath.Join(cwd, ".mygo", "cli-profile")
	t.Setenv("ROUTER_FORGE_DATA_DIR", envPath)
	options, err := locateBackendAt(cliPath)
	if err != nil || options.DataDir != cliPath {
		t.Fatalf("CLI data directory not passed to backend Options: %v", err)
	}
	options, err = locateBackend()
	if err != nil || options.DataDir != envPath {
		t.Fatalf("existing environment override was lost: %v", err)
	}
}

func TestAPITransportRejectsNonLocalPaths(t *testing.T) {
	for _, path := range []string{"https://example.test/api/status", "//example.test/api/status", "/file", "/api/../file", "/api/%2e%2e/file", "/api/%2F%2Fevil", "/api/a\\b", "/api/a//b", "/api/%252e%252e/file"} {
		if err := validateAPIRequest("GET", path); err == nil {
			t.Errorf("accepted forbidden path %q", path)
		}
	}
	if err := validateAPIRequest("DELETE", "/api/runs/one"); err == nil {
		t.Fatal("accepted destructive arbitrary method")
	}
	if err := validateAPIRequest("GET", "/api/requests?run_id=run_one&limit=50&offset=0"); err != nil {
		t.Fatal(err)
	}
}

func TestBackendOriginIsLoopback(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:1234", "http://example.test:1234", "http://127.0.0.1:1234/elsewhere", "http://user:pass@127.0.0.1:1234", "http://127.0.0.1"} {
		if validateBackendURL(origin) == nil {
			t.Errorf("accepted forbidden origin %q", origin)
		}
	}
	if err := validateBackendURL("http://127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCredentialDoesNotReachRenderer(t *testing.T) {
	const credential = "native-only-test-credential"
	const writeOnlyKey = "write-only-test-value"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Error("control authentication missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"` + writeOnlyKey + `","key_hint":"…value","key_revision":"revision-test","error":"Bearer ` + credential + `"}`))
	}))
	defer upstream.Close()
	console := newConsole()
	console.options = func() (backend.Options, error) { return backend.Options{}, nil }
	console.ensure = func(context.Context, backend.Options) (domain.BackendInfo, error) {
		return domain.BackendInfo{URL: upstream.URL, ControlToken: credential, PID: 42, Version: "test"}, nil
	}
	connection, err := console.Connect()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(connection)
	if strings.Contains(string(encoded), credential) || strings.Contains(string(encoded), "control_token") {
		t.Fatal("connection leaked control credential")
	}
	response, err := console.Request(context.Background(), "POST", "/api/providers", `{"api_key":"`+writeOnlyKey+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body, credential) || strings.Contains(response.Body, writeOnlyKey) {
		t.Fatal("native response leaked write-only credentials")
	}
	if !strings.Contains(response.Body, "revision-test") || !strings.Contains(response.Body, "key_hint") {
		t.Fatal("public key metadata was removed")
	}
}

func TestRedirectDoesNotForwardControlCredential(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/status", http.StatusFound)
	}))
	defer server.Close()
	console := newConsole()
	console.info = domain.BackendInfo{URL: server.URL, ControlToken: "local-test"}
	response, err := console.Request(context.Background(), "GET", "/api/status", "")
	if err != nil {
		t.Fatal(err)
	}
	if reached || response.Status != http.StatusFound {
		t.Fatal("redirect was followed")
	}
}

func TestRedactionNestedDiagnosticsAndExactNumbers(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"error":  `failed {\"api_key\":\"escaped secret with spaces\"}`,
		"nested": `{"apiKey":"nested-test-secret","input_tokens":9007199254740993}`,
		"known":  "quoted-test-secret",
	})
	redacted := redactResponse(string(body), []string{"quoted-test-secret"})
	for _, secret := range []string{"escaped secret", "nested-test-secret", "quoted-test-secret"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("diagnostic leaked %q", secret)
		}
	}
	if !strings.Contains(redacted, "9007199254740993") {
		t.Fatal("JSON number precision was altered")
	}
}

func TestRedactionPreservesUnknownUsage(t *testing.T) {
	body := redactResponse(`{"usage":{"input_tokens":null,"cache_read_tokens":0},"cost_amount":null,"nested":{"access_token":"test-secret"}}`, nil)
	if !strings.Contains(body, `"input_tokens":null`) || !strings.Contains(body, `"cache_read_tokens":0`) || strings.Contains(body, "test-secret") {
		t.Fatal("redaction altered metering or exposed a credential")
	}
}

func TestNativePreflightAllowsTheControlAPIBudget(t *testing.T) {
	console := newConsole()
	console.info = domain.BackendInfo{URL: "http://127.0.0.1:1234", ControlToken: "fixture-only-control"}
	console.client.Transport = apiTestTransport(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) < 3*time.Minute {
			t.Error("native request expires before the control API's three-minute preflight budget")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	if _, err := console.Request(context.Background(), http.MethodPost, "/api/runs", `{}`); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRequestTimeoutIsNotABackendDisconnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	console := newConsole()
	console.info = domain.BackendInfo{URL: server.URL, ControlToken: "fixture-only-control"}
	defer console.client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := console.Request(ctx, http.MethodPost, "/api/runs", `{}`)
	if err == nil || !strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "unavailable") {
		t.Fatal("a bounded preflight timeout was misreported as a lost backend")
	}
}

type apiTestTransport func(*http.Request) (*http.Response, error)

func (f apiTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

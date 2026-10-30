package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type networkPrivacyTransport func(*http.Request) (*http.Response, error)

func (f networkPrivacyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConsoleProxyCredentialsNeverReturnToRenderer(t *testing.T) {
	username, password := "proxy-user-sensitive", "proxy-password-sensitive"
	basic := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	console := newConsole()
	console.info = domain.BackendInfo{URL: "http://127.0.0.1:19341", ControlToken: strings.Repeat("control", 8)}
	console.client.Transport = networkPrivacyTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+console.info.ControlToken {
			t.Fatal("native control authentication was not preserved")
		}
		body, _ := json.Marshal(map[string]any{
			"error":          username + " " + password,
			"proxy_username": username,
			"proxy_password": password,
			"detail":         "Proxy-Authorization: Basic " + basic,
			"profile":        map[string]any{"name": "Visible profile", "proxy_credentials": map[string]string{"username": username, "password": password}},
		})
		return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	input, _ := json.Marshal(map[string]any{"profile": map[string]string{"name": "Visible profile"}, "proxy_username": username, "proxy_password": password})
	response, err := console.Request(context.Background(), "POST", "/api/network-profiles", string(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{username, password, basic, console.info.ControlToken} {
		if strings.Contains(response.Body, secret) {
			t.Fatal("private network data returned through the native bridge")
		}
	}
	if response.Status != http.StatusBadRequest || !strings.Contains(response.Body, "Visible profile") {
		t.Fatal("redaction must preserve status and public profile data")
	}
}

func TestSuccessfulNetworkResponsePreservesPublicConfiguration(t *testing.T) {
	body := `{"id":"net_1","revision":"rev_1","scope":"environment","proxy_mode":"proxy","name":"a network","ca_cert_path":"/home/alice/ca.pem","proxy_username":"alice","proxy_password":"a","detail":"alice a"}`
	result := redactNetworkProfileResponse(body, nil, []string{"alice", "a"})
	var profile map[string]any
	if err := json.Unmarshal([]byte(result), &profile); err != nil {
		t.Fatal(err)
	}
	if profile["name"] != "a network" || profile["ca_cert_path"] != "/home/alice/ca.pem" || profile["revision"] != "rev_1" {
		t.Fatal("credential filtering corrupted the public network configuration")
	}
	if profile["proxy_username"] != "[redacted]" || profile["proxy_password"] != "[redacted]" || strings.Contains(profile["detail"].(string), "alice") {
		t.Fatal("network response contained private diagnostic or credential fields")
	}
}

func TestRedactProxyAssignmentsAndBasicAuthentication(t *testing.T) {
	for _, input := range []string{
		`proxy_username="proxy-user-sensitive" proxy_password='proxy-password-sensitive'`,
		`Proxy-Authorization: Basic cHJpdmF0ZS11c2VyOnByaXZhdGUtcGFzc3dvcmQ=`,
		`{"detail":{"proxy_username":"proxy-user-sensitive","proxy_password":"proxy-password-sensitive"}}`,
		`https://proxy-user-sensitive:proxy-password-sensitive@proxy.example:443`,
	} {
		result := redactResponse(input, nil)
		for _, secret := range []string{"proxy-user-sensitive", "proxy-password-sensitive", "cHJpdmF0ZS11c2VyOnByaXZhdGUtcGFzc3dvcmQ="} {
			if strings.Contains(result, secret) {
				t.Fatal("redaction left proxy authentication in a response")
			}
		}
	}
}

package executor

import (
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestProfileTLSSkipIsExplicitScopedAndIgnoresInactiveCA(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tls-fixture") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	directory := projectTestDir(t)
	certificate := filepath.Join(directory, "fixture-ca.pem")
	if err := writePrivate(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		env  domain.Environment
		ok   bool
	}{
		{"strict_default", domain.Environment{}, false},
		{"trusted_CA", domain.Environment{CACertPath: certificate}, true},
		{"explicit_skip", domain.Environment{SkipTLSVerification: true}, true},
		{"skip_ignores_missing_CA", domain.Environment{SkipTLSVerification: true, CACertPath: filepath.Join(directory, "missing.pem")}, true},
		{"strict_restored", domain.Environment{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := profileClient(tc.env)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			response, err := client.Get(server.URL)
			if !tc.ok {
				if err == nil {
					response.Body.Close()
					t.Fatal("an untrusted TLS server was accepted without explicit opt-in")
				}
				return
			}
			if err != nil {
				t.Fatal("explicit policy was not applied to real HTTPS", err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "tls-fixture" {
				t.Fatal("wrong TLS fixture response", err)
			}
		})
	}
}

func TestHarborTLSSkipReplacesInactiveCAMountWithoutChangingApprovedSpec(t *testing.T) {
	spec := fixtureSpec()
	spec.Snapshot.Network.Agent.CACertPath = "/missing/retained-ca.pem"
	spec.Snapshot.Network.Agent.TrustMode = domain.NetworkCustomCA
	spec.Snapshot.Network.Agent.TLSMode = domain.NetworkSkipTLS
	spec.Snapshot.Network.Agent.TLSRiskAccepted = true
	sealFixtureSpec(&spec)
	before := spec.Snapshot.ConfigHash
	config, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/compose.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range config.Environment.Mounts {
		if mount.Source == spec.Snapshot.Network.Agent.CACertPath || mount.Target == "/etc/router-forge/ca.pem" {
			t.Fatal("inactive CA was still mounted")
		}
	}
	for _, env := range []map[string]string{config.Agents[0].Env} {
		if env["GIT_SSL_NO_VERIFY"] != "true" || env["NODE_TLS_REJECT_UNAUTHORIZED"] != "0" || env["npm_config_strict_ssl"] != "false" || env["CURL_HOME"] != containerNetworkRoot+"/agent" {
			t.Fatal("native installer TLS policy missing", env)
		}
		for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
			if _, present := env[key]; present {
				t.Fatal("inactive CA still reached a TLS client", key)
			}
		}
	}
	if len(config.Environment.Env) != 0 || config.Verifier.Env["npm_config_strict_ssl"] != "true" || config.Verifier.Env["ROUTER_FORGE_SKIP_TLS_VERIFICATION"] != "" || config.Verifier.Env["CURL_HOME"] != containerNetworkRoot+"/verifier" {
		t.Fatal("agent TLS relaxation reached the original verifier", config.Verifier.Env)
	}
	if spec.Snapshot.Network.Agent.CACertPath != "/missing/retained-ca.pem" || spec.Snapshot.ConfigHash != before || spec.Snapshot.ValidateSources() != nil {
		t.Fatal("building client policy changed the approved configuration")
	}
}

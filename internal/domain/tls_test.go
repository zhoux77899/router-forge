package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvironmentTLSDefaultsAndPreservesCA(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		skip bool
	}{
		{"historical", `{"id":"env","ca_cert_path":"/missing/retained.pem"}`, false},
		{"strict", `{"id":"env","ca_cert_path":"/missing/retained.pem","skip_tls_verification":false}`, false},
		{"explicit_skip", `{"id":"env","ca_cert_path":"/missing/retained.pem","skip_tls_verification":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var environment Environment
			if err := json.Unmarshal([]byte(tc.json), &environment); err != nil {
				t.Fatal(err)
			}
			if environment.SkipTLSVerification != tc.skip || environment.CACertPath != "/missing/retained.pem" {
				t.Fatalf("TLS policy or retained CA changed: %+v", environment)
			}
			data, err := json.Marshal(environment)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.skip && strings.Contains(string(data), "skip_tls_verification") {
				t.Fatal("default-false field changed historical canonical JSON")
			}
			var roundTrip Environment
			if err := json.Unmarshal(data, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if roundTrip.SkipTLSVerification != tc.skip || roundTrip.CACertPath != environment.CACertPath {
				t.Fatal("TLS policy was lost during JSON round-trip")
			}
		})
	}
	var environment Environment
	if json.Unmarshal([]byte(`{"skip_tls_verification":"false"}`), &environment) == nil {
		t.Fatal("a string silently changed the TLS policy")
	}
}

func TestApprovalHashBindsTLSVerificationPolicy(t *testing.T) {
	snapshot := snapshotFixture(t)
	strictHash := snapshot.ConfigHash
	snapshot.Environment.SkipTLSVerification = true
	skipHash, err := snapshot.ApprovalHash()
	if err != nil || skipHash == strictHash {
		t.Fatal("TLS skip was excluded from the approved configuration", err)
	}
	if snapshot.ValidateSources() == nil {
		t.Fatal("TLS verification could be disabled after approval")
	}
	snapshot.ConfigHash = skipHash
	if err := snapshot.ValidateSources(); err != nil {
		t.Fatal(err)
	}
	snapshot.Environment.SkipTLSVerification = false
	if snapshot.ValidateSources() == nil {
		t.Fatal("the approved TLS policy could be silently changed at start")
	}
	current, err := snapshot.ApprovalHash()
	if err != nil || current != strictHash {
		t.Fatal("restoring strict mode changed the historical default hash", err)
	}
}

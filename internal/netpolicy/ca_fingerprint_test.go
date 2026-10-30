package netpolicy

import (
	"crypto/sha256"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestCACertSHA256UsesFrozenLoadedBytes(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "selected CA trust")
	})
	first := newTLSFixture(t, handler)
	second := newTLSFixture(t, handler)
	firstBytes := append([]byte("selected custom CA file\n"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: first.Certificate().Raw})...)
	firstBytes = append(firstBytes, '\n', '\n')
	secondBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: second.Certificate().Raw})
	path := writeFixture(t, firstBytes)
	policy := directPolicy()
	policy.TrustMode, policy.CACertPath = "custom", path
	firstClient := clientFor(t, policy)
	firstTransport := firstClient.Transport.(*Transport)
	firstHash := fmt.Sprintf("%x", sha256.Sum256(firstBytes))
	if firstTransport.CACertSHA256() != firstHash {
		t.Fatal("CA fingerprint did not hash the exact accepted file bytes")
	}
	if err := os.WriteFile(path, secondBytes, 0600); err != nil {
		t.Fatal("cannot replace local CA fixture")
	}
	secondClient := clientFor(t, policy)
	secondTransport := secondClient.Transport.(*Transport)
	secondHash := fmt.Sprintf("%x", sha256.Sum256(secondBytes))
	if secondTransport.CACertSHA256() != secondHash || firstHash == secondHash {
		t.Fatal("new CA snapshot did not fingerprint its own loaded bytes")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("cannot remove local CA fixture")
	}
	policy.CACertPath = "absent-mutated-profile-ca.pem"
	if firstTransport.CACertSHA256() != firstHash || secondTransport.CACertSHA256() != secondHash {
		t.Fatal("CA fingerprint reread a removed path or inherited profile mutation")
	}
	for _, check := range []struct {
		clientURL string
		client    *http.Client
		trusted   bool
	}{
		{first.URL, firstClient, true},
		{second.URL, secondClient, true},
		{second.URL, firstClient, false},
		{first.URL, secondClient, false},
	} {
		response, err := check.client.Get(check.clientURL)
		if !check.trusted {
			if err == nil {
				_ = response.Body.Close()
				t.Fatal("frozen fingerprint did not match the active custom CA trust")
			}
			continue
		}
		if err != nil {
			t.Fatal("loaded custom CA did not authorize its own self-signed target")
		}
		if readBody(t, response) != "selected CA trust" {
			t.Fatal("custom trust fixture response changed")
		}
	}
}

func TestCACertSHA256EmptyWhenCustomCANotLoaded(t *testing.T) {
	server := newTLSFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	validCA := certificateFile(t, server)
	invalidCA := writeFixture(t, []byte("invalid retained custom CA"))
	for _, check := range []struct {
		name      string
		tlsMode   string
		trustMode string
		path      string
	}{
		{"system retains valid CA", "verify", "system", validCA},
		{"system ignores missing CA", "verify", "system", "absent-system-ca.pem"},
		{"skip ignores valid CA", "skip", "custom", validCA},
		{"skip ignores missing CA", "skip", "custom", "absent-skip-ca.pem"},
		{"skip ignores invalid CA", "skip", "custom", invalidCA},
		{"skip does not open a directory", "skip", "custom", "."},
		{"skip with system trust", "skip", "system", validCA},
	} {
		t.Run(check.name, func(t *testing.T) {
			policy := directPolicy()
			policy.TLSMode, policy.TrustMode, policy.CACertPath = check.tlsMode, check.trustMode, check.path
			policy.TLSRiskAccepted = check.tlsMode == "skip"
			client := clientFor(t, policy)
			if client.Transport.(*Transport).CACertSHA256() != "" {
				t.Fatal("CA fingerprint exposed an ignored or unread CA")
			}
			response, err := client.Get(server.URL)
			if check.tlsMode == "verify" {
				if err == nil {
					_ = response.Body.Close()
					t.Fatal("retained CA affected explicit system trust")
				}
				return
			}
			if err != nil {
				t.Fatal("confirmed skip loaded an ignored CA or inherited strict trust")
			}
			_ = response.Body.Close()
		})
	}
}

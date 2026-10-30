package netpolicy

import (
	"crypto/x509"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const systemTrustChild = "NETPOLICY_ROOT_TEST_CHILD"

func TestSystemTrustIgnoresAmbientCA(t *testing.T) {
	if mode := os.Getenv(systemTrustChild); mode != "" {
		testSystemTrustChild(t, mode)
		return
	}
	for _, mode := range []string{"file", "directory", "cached-file", "cached-directory", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal("cannot locate root-trust test executable")
			}
			command := exec.Command(executable, "-test.run=^TestSystemTrustIgnoresAmbientCA$", "-test.timeout=20s")
			command.Env = append(os.Environ(), systemTrustChild+"="+mode)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("isolated root-trust check failed: %s", output)
			}
		})
	}
}

func testSystemTrustChild(t *testing.T, mode string) {
	t.Helper()
	server := newTLSFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ambientCA := certificateFile(t, server)
	other := newTLSFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	explicitCA := certificateFile(t, other)
	if strings.Contains(mode, "directory") {
		t.Setenv("SSL_CERT_FILE", "absent-system-root-fixture.pem")
		t.Setenv("SSL_CERT_DIR", ".")
	} else {
		t.Setenv("SSL_CERT_FILE", ambientCA)
		t.Setenv("SSL_CERT_DIR", "absent-system-root-directory")
	}
	if mode == "fallback" {
		t.Setenv("GODEBUG", "x509usefallbackroots=1")
		fallback := x509.NewCertPool()
		fallback.AddCert(server.Certificate())
		x509.SetFallbackRoots(fallback)
	}
	if strings.HasPrefix(mode, "cached") {
		if _, err := x509.SystemCertPool(); err != nil {
			t.Fatal("cannot initialize ambient root-cache fixture")
		}
		t.Setenv("SSL_CERT_FILE", "")
		t.Setenv("SSL_CERT_DIR", "")
	}
	for _, trustMode := range []string{"system", "custom"} {
		policy := directPolicy()
		policy.TrustMode = trustMode
		if trustMode == "custom" {
			policy.CACertPath = explicitCA
		}
		client, err := NewClient(policy)
		if err != nil {
			if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
				continue
			}
			t.Fatal("OS trust snapshot could not be loaded")
		}
		client.Timeout = testTimeout
		t.Cleanup(client.CloseIdleConnections)
		response, err := client.Get(server.URL)
		if err == nil {
			_ = response.Body.Close()
			t.Errorf("%s trust accepted an ambient-only CA", trustMode)
		}
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return
	}
	policy := directPolicy()
	policy.TrustMode, policy.CACertPath = "custom", explicitCA
	response, err := clientFor(t, policy).Get(other.URL)
	if err != nil {
		t.Fatal("explicit custom CA stopped working")
	}
	_ = response.Body.Close()
}

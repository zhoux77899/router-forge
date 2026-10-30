package control

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestEnvironmentTLSOptInRoundTripsThroughAdminAPIWithoutLosingCA(t *testing.T) {
	f := setup(t, false)
	code, body := f.request(t, "POST", "/api/environments", map[string]any{"name": "Legacy strict", "type": "local"}, true)
	environment := result[domain.Environment](t, code, body)
	if environment.SkipTLSVerification {
		t.Fatal("an omitted TLS field did not default to strict")
	}
	ca := filepath.Join(t.TempDir(), "retained-ca.pem")
	for _, skip := range []bool{true, false} {
		environment.SkipTLSVerification, environment.CACertPath = skip, ca
		code, body = f.request(t, "POST", "/api/environments", environment, true)
		saved := result[domain.Environment](t, code, body)
		if saved.ID != environment.ID || saved.CACertPath != ca || saved.SkipTLSVerification != skip || saved.CreatedAt != environment.CreatedAt {
			t.Fatal("saved TLS opt-in lost CA or changed profile identity", saved)
		}
		code, body = f.request(t, "GET", "/api/environments", nil, true)
		loaded := result[[]domain.Environment](t, code, body)
		if len(loaded) != 1 || loaded[0].CACertPath != ca || loaded[0].SkipTLSVerification != skip {
			t.Fatal("the environment list did not return the saved TLS policy", loaded)
		}
	}
	code, _ = f.request(t, "POST", "/api/environments", map[string]any{"name": "Invalid boolean", "type": "local", "skip_tls_verification": "false"}, true)
	if code != http.StatusBadRequest {
		t.Fatal("the API silently coerced a non-boolean TLS field", code)
	}
	code, _ = f.request(t, "POST", "/api/environments", domain.Environment{Name: "Unauthorized TLS change", Type: "local", SkipTLSVerification: true}, false)
	if code != http.StatusUnauthorized {
		t.Fatal("a readonly client could change an environment TLS policy", code)
	}
}

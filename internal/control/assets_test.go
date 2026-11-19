package control

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAssetAncestorAndFallbackSymlinksAreRejected(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "asset-ancestor", true: "SPA-index"}[fallback], func(t *testing.T) {
			f := setup(t, false)
			root := t.TempDir()
			web := filepath.Join(root, "web")
			private := filepath.Join(root, "private")
			for _, dir := range []string{web, private} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(private, "secret.txt"), []byte("private-fixture-must-not-be-served"), 0600); err != nil {
				t.Fatal(err)
			}
			path := "/assets/secret.txt"
			var err error
			if fallback {
				err = os.Symlink(filepath.Join(private, "secret.txt"), filepath.Join(web, "index.html"))
				path = "/unknown-client-route"
			} else {
				if err = os.WriteFile(filepath.Join(web, "index.html"), []byte("<h1>Readonly results</h1>"), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(private, filepath.Join(web, "assets"))
			}
			if err != nil {
				t.Skipf("platform cannot create symlink fixture: %v", err)
			}
			f.app.config.WebDir = web
			code, body := f.request(t, "GET", path, nil, false)
			if code != 403 || strings.Contains(string(body), "private-fixture-must-not-be-served") {
				t.Fatalf("static scope bypass: HTTP %d %s", code, body)
			}
		})
	}
}

func TestWebFontSourceCSP(t *testing.T) {
	f := setup(t, false)
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<h1>Readonly results</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	f.app.config.WebDir = web
	for _, path := range []string{"/", "/results/run_fixture"} {
		t.Run(path, func(t *testing.T) {
			response, err := http.Get(f.http.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("results page: HTTP %d", response.StatusCode)
			}
			policy := make(map[string]string)
			for _, directive := range strings.Split(response.Header.Get("Content-Security-Policy"), ";") {
				fields := strings.Fields(directive)
				if len(fields) > 0 {
					policy[fields[0]] = strings.Join(fields[1:], " ")
				}
			}
			for _, expected := range []struct{ name, sources string }{
				{"style-src", "'self' 'unsafe-inline' https://cdn.jsdelivr.net/fontsource/css/ https://cdn.jsdelivr.net/npm/@fontsource-variable/noto-sans-sc@5.3.1/index.css"},
				{"font-src", "'self' https://cdn.jsdelivr.net/fontsource/fonts/ https://cdn.jsdelivr.net/npm/@fontsource-variable/noto-sans-sc@5.3.1/files/"},
				{"default-src", "'self'"},
				{"script-src", "'self'"},
				{"connect-src", "'self'"},
				{"img-src", "'self' data:"},
				{"frame-ancestors", "'none'"},
				{"base-uri", "'none'"},
			} {
				if got := policy[expected.name]; got != expected.sources {
					t.Errorf("%s = %q; want %q", expected.name, got, expected.sources)
				}
			}
		})
	}
	if f.calls.Load() != 0 {
		t.Fatal("serving the results UI made a provider request")
	}
}

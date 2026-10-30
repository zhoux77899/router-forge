package control

import (
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

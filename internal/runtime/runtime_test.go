package runtime

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("RF_RUNTIME_TEST_HELPER") == "1" {
		helper()
		return
	}
	os.Exit(m.Run())
}
func helper() {
	flags := flag.NewFlagSet("helper", flag.ExitOnError)
	data := flags.String("data-dir", "", "")
	listen := flags.String("listen", "127.0.0.1:0", "")
	flags.String("workers-dir", "", "")
	flags.String("web-dir", "", "")
	flags.String("dev-origin", "", "")
	flags.Parse(os.Args[1:])
	if err := store.EnsurePrivateDir(*data); err != nil {
		panic(err)
	}
	lock := flock.New(filepath.Join(*data, "server.lock"))
	if ok, err := lock.TryLock(); err != nil || !ok {
		panic("helper instance lock")
	}
	defer lock.Unlock()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		panic(err)
	}
	info := domain.BackendInfo{URL: "http://" + listener.Addr().String(), ControlToken: domain.NewID("control"), PID: os.Getpid(), Version: domain.Version, DataDir: *data}
	b, _ := json.Marshal(info)
	if err := store.WritePrivateFile(filepath.Join(*data, "backend.json"), b); err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+info.ControlToken {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"running": true, "version": domain.Version, "pid": os.Getpid()})
	})
	if err := http.Serve(listener, mux); err != nil {
		panic(err)
	}
}

func TestBackgroundSurvivesCallerAndReattaches(t *testing.T) {
	t.Setenv("RF_RUNTIME_TEST_HELPER", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "profile")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	info, err := EnsureRunning(ctx, Options{DataDir: dir, ServerPath: binary})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process, err := os.FindProcess(info.PID)
		if err == nil {
			process.Kill()
		}
		time.Sleep(50 * time.Millisecond)
	})
	cancel()
	if !Healthy(context.Background(), info) {
		t.Fatal("backend stopped when caller context ended")
	}
	second, err := EnsureRunning(context.Background(), Options{DataDir: dir, ServerPath: "does-not-exist"})
	if err != nil {
		t.Fatal(err)
	}
	if second.PID != info.PID || second.ControlToken != info.ControlToken {
		t.Fatal("reattach started a duplicate instance")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "backend.log"))
	if strings.Contains(string(b), info.ControlToken) {
		t.Fatal("control token appeared in logs")
	}
}

func TestUnsafeDescriptorNeverContactsRemoteEndpoint(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	if err := store.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"https://example.test:443", "http://localhost:8080", "http://127.0.0.1:8080/path", "http://user:pass@127.0.0.1:8080", "http://127.0.0.1:8080?token=x"} {
		info := domain.BackendInfo{URL: address, ControlToken: domain.NewID("control"), PID: os.Getpid(), Version: domain.Version, DataDir: dir}
		b, _ := json.Marshal(info)
		if err := store.WritePrivateFile(filepath.Join(dir, "backend.json"), b); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadInfo(dir); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
}

func TestProfileWithHeldInstanceLockDoesNotSpawn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	if err := store.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(filepath.Join(dir, "server.lock"))
	ok, err := lock.TryLock()
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer lock.Unlock()
	_, err = EnsureRunning(context.Background(), Options{DataDir: dir, ServerPath: "unexpected"})
	if err == nil || !strings.Contains(err.Error(), "already") && !strings.Contains(err.Error(), "existing") {
		t.Fatal(fmt.Sprint(err))
	}
}

func TestResolvePathsForBundledAndWorkspaceAssets(t *testing.T) {
	t.Setenv("ROUTER_FORGE_SERVER", "")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if strings.HasSuffix(strings.ToLower(binary), ".exe") {
		suffix = ".exe"
	}
	for _, layout := range []struct {
		name      string
		serverDir []string
		webDir    []string
	}{
		{"bundle", []string{"server"}, []string{"dist"}},
		{"workspace", []string{"build", "server"}, []string{"apps", "web", "dist"}},
		{"custom-web", []string{"server"}, []string{"web"}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			root := t.TempDir()
			serverDir := filepath.Join(append([]string{root}, layout.serverDir...)...)
			webDir := filepath.Join(append([]string{root}, layout.webDir...)...)
			for _, dir := range []string{serverDir, webDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			server := filepath.Join(serverDir, "router-forge-server"+suffix)
			if err := os.WriteFile(server, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			options := ResolvePaths(root)
			if options.ServerPath != server || options.WebDir != webDir || options.WorkerDir != filepath.Join(filepath.Dir(serverDir), "workers") {
				t.Fatalf("incorrect %s resource paths: %+v", layout.name, options)
			}
		})
	}
}

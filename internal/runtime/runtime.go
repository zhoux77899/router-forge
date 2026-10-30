package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/store"
)

type Options struct {
	DataDir    string
	ServerPath string
	WorkerDir  string
	WebDir     string
	DevOrigin  string
}

func DefaultDataDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "RouterForge"), nil
}

func ReadInfo(dataDir string) (domain.BackendInfo, error) {
	var info domain.BackendInfo
	b, err := os.ReadFile(filepath.Join(dataDir, "backend.json"))
	if err != nil {
		return info, err
	}
	if len(b) > 16384 {
		return info, fmt.Errorf("invalid backend descriptor")
	}
	if err = json.Unmarshal(b, &info); err != nil {
		return info, err
	}
	u, err := url.Parse(info.URL)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Port() == "" || info.PID < 1 || info.ControlToken == "" {
		return domain.BackendInfo{}, fmt.Errorf("invalid local backend descriptor")
	}
	abs, _ := filepath.Abs(dataDir)
	if info.DataDir != abs {
		return domain.BackendInfo{}, fmt.Errorf("backend descriptor belongs to another data directory")
	}
	return info, nil
}

func Healthy(ctx context.Context, info domain.BackendInfo) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL+"/api/status", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+info.ControlToken)
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var state struct {
		Version string `json:"version"`
		PID     int    `json:"pid"`
		Running bool   `json:"running"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&state) != nil {
		return false
	}
	return state.Running && state.PID == info.PID && state.Version == domain.Version && info.Version == domain.Version
}

func EnsureRunning(ctx context.Context, options Options) (domain.BackendInfo, error) {
	if options.DataDir == "" {
		dir, err := DefaultDataDir()
		if err != nil {
			return domain.BackendInfo{}, err
		}
		options.DataDir = dir
	}
	abs, err := filepath.Abs(options.DataDir)
	if err != nil {
		return domain.BackendInfo{}, err
	}
	options.DataDir = abs
	if err = store.EnsurePrivateDir(abs); err != nil {
		return domain.BackendInfo{}, err
	}
	startupPath := filepath.Join(abs, "startup.lock")
	if err = store.EnsurePrivateFile(startupPath); err != nil {
		return domain.BackendInfo{}, err
	}
	startup := flock.New(startupPath)
	locked, err := startup.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return domain.BackendInfo{}, err
	}
	if !locked {
		return domain.BackendInfo{}, fmt.Errorf("another Desktop instance is starting the backend")
	}
	defer startup.Unlock()
	if info, err := ReadInfo(abs); err == nil && Healthy(ctx, info) {
		return info, nil
	}
	lockPath := filepath.Join(abs, "server.lock")
	if err = store.EnsurePrivateFile(lockPath); err != nil {
		return domain.BackendInfo{}, err
	}
	probe := flock.New(lockPath)
	free, err := probe.TryLock()
	if err != nil {
		return domain.BackendInfo{}, err
	}
	if !free {
		return domain.BackendInfo{}, fmt.Errorf("an existing backend owns this profile but is not responding; no duplicate process was started")
	}
	probe.Unlock()
	if options.ServerPath == "" {
		return domain.BackendInfo{}, fmt.Errorf("the bundled backend executable could not be located; run bun run build:backend")
	}
	if _, err = os.Stat(options.ServerPath); err != nil {
		return domain.BackendInfo{}, fmt.Errorf("backend executable is unavailable: %w", err)
	}
	logPath := filepath.Join(abs, "backend.log")
	if err = store.EnsurePrivateFile(logPath); err != nil {
		return domain.BackendInfo{}, err
	}
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return domain.BackendInfo{}, err
	}
	defer log.Close()
	args := []string{"--data-dir", abs, "--listen", "127.0.0.1:0"}
	if options.WorkerDir != "" {
		args = append(args, "--workers-dir", options.WorkerDir)
	}
	if options.WebDir != "" {
		args = append(args, "--web-dir", options.WebDir)
	}
	if options.DevOrigin != "" {
		args = append(args, "--dev-origin", options.DevOrigin)
	}
	// Deliberately not CommandContext: the backend outlives its renderer and startup request.
	cmd := exec.Command(options.ServerPath, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	detach(cmd)
	if err = cmd.Start(); err != nil {
		return domain.BackendInfo{}, fmt.Errorf("start backend: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		if info, err := ReadInfo(abs); err == nil && Healthy(ctx, info) {
			return info, nil
		}
		select {
		case err := <-done:
			return domain.BackendInfo{}, fmt.Errorf("backend exited before readiness (%v); see the private backend.log", err)
		case <-ctx.Done():
			return domain.BackendInfo{}, ctx.Err()
		case <-deadline.C:
			return domain.BackendInfo{}, fmt.Errorf("backend did not become ready within 20 seconds; see the private backend.log")
		case <-ticker.C:
		}
	}
}

func ResolvePaths(startDir string) Options {
	if path := os.Getenv("ROUTER_FORGE_SERVER"); path != "" {
		return Options{ServerPath: path, WorkerDir: os.Getenv("ROUTER_FORGE_WORKERS"), WebDir: os.Getenv("ROUTER_FORGE_WEB"), DataDir: os.Getenv("ROUTER_FORGE_DATA_DIR")}
	}
	exe, _ := os.Executable()
	suffix := ""
	if strings.HasSuffix(strings.ToLower(exe), ".exe") {
		suffix = ".exe"
	}
	for _, root := range []string{filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "resources"), startDir, filepath.Join(startDir, "..", "..")} {
		for _, candidate := range []string{filepath.Join(root, "server", "router-forge-server"+suffix), filepath.Join(root, "build", "server", "router-forge-server"+suffix)} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				base := filepath.Dir(filepath.Dir(candidate))
				web := ""
				for _, candidate := range []string{filepath.Join(root, "web"), filepath.Join(root, "dist"), filepath.Join(base, "dist"), filepath.Join(root, "apps", "web", "dist")} {
					if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
						web = candidate
						break
					}
				}
				return Options{ServerPath: candidate, WorkerDir: filepath.Join(base, "workers"), WebDir: web, DataDir: os.Getenv("ROUTER_FORGE_DATA_DIR")}
			}
		}
	}
	return Options{DataDir: os.Getenv("ROUTER_FORGE_DATA_DIR")}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/zhoux77899/router-forge/internal/domain"
	backend "github.com/zhoux77899/router-forge/internal/runtime"
)

// Connection deliberately excludes the host control credential.
type Connection struct {
	URL       string `json:"url"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	DataDir   string `json:"data_dir"`
	WorkerDir string `json:"worker_dir"`
}

type Response struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// Console connects to an independent host backend; window lifetime never owns a job.
type Console struct {
	mu      sync.Mutex
	info    domain.BackendInfo
	options func() (backend.Options, error)
	ensure  func(context.Context, backend.Options) (domain.BackendInfo, error)
	client  *http.Client
}

func newConsole() *Console {
	return &Console{
		options: locateBackend,
		ensure:  backend.EnsureRunning,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: 185 * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       200 * time.Second,
		},
	}
}

// Connect starts or attaches to the detached backend and returns only public metadata.
func (c *Console) Connect() (Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	options, err := c.options()
	if err != nil {
		return Connection{}, errors.New("cannot locate backend resources; build the host helpers and read-only web app first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	info, err := c.ensure(ctx, options)
	if err != nil {
		return Connection{}, errors.New("cannot attach to the host backend; check helper binaries and data-directory permissions")
	}
	if err := validateBackendURL(info.URL); err != nil {
		return Connection{}, err
	}
	c.info = info
	return Connection{info.URL, info.PID, info.Version, info.DataDir, info.WorkerDir}, nil
}

// Request is a bounded loopback-only API transport, not a URL or filesystem proxy.
func (c *Console) Request(ctx context.Context, method, path, body string) (Response, error) {
	if err := validateAPIRequest(method, path); err != nil {
		return Response{}, err
	}
	if len(body) > 1<<20 {
		return Response{}, errors.New("API request is too large")
	}
	c.mu.Lock()
	info := c.info
	c.mu.Unlock()
	if info.URL == "" {
		return Response{}, errors.New("connect to the host backend first")
	}
	if err := validateBackendURL(info.URL); err != nil {
		return Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(info.URL, "/")+path, strings.NewReader(body))
	if err != nil {
		return Response{}, errors.New("cannot construct local API request")
	}
	request.Header.Set("Authorization", "Bearer "+info.ControlToken)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Response{}, errors.New("local API request was cancelled")
		}
		var timedOut net.Error
		if errors.As(err, &timedOut) && timedOut.Timeout() {
			return Response{}, errors.New("host backend request timed out; check environment or benchmark preflight diagnostics")
		}
		return Response{}, errors.New("host backend is unavailable; reconnect to attach again")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return Response{}, errors.New("host backend returned an unreadable or oversized response")
	}
	secrets := []string{info.ControlToken}
	var input struct {
		APIKey        *string `json:"api_key"`
		ProxyUsername *string `json:"proxy_username"`
		ProxyPassword *string `json:"proxy_password"`
	}
	proxySecrets := []string{}
	if json.Unmarshal([]byte(body), &input) == nil {
		if input.APIKey != nil && *input.APIKey != "" {
			secrets = append(secrets, *input.APIKey)
		}
		for _, secret := range []*string{input.ProxyUsername, input.ProxyPassword} {
			if secret != nil && *secret != "" {
				proxySecrets = append(proxySecrets, *secret)
			}
		}
	}
	result := redactResponse(string(data), append(append([]string{}, secrets...), proxySecrets...))
	if strings.HasPrefix(path, "/api/network-profiles") && response.StatusCode >= 200 && response.StatusCode < 300 {
		result = redactNetworkProfileResponse(string(data), secrets, proxySecrets)
	}
	return Response{Status: response.StatusCode, Body: result}, nil
}

func validateBackendURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("backend must be an HTTP loopback origin")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("backend must be an HTTP loopback origin")
	}
	if u.Port() == "" {
		return errors.New("backend must include its local port")
	}
	return nil
}

func validateAPIRequest(method, path string) error {
	if method != http.MethodGet && method != http.MethodPost {
		return errors.New("unsupported API method")
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(path, "/api/") || !strings.HasPrefix(u.Path, "/api/") || strings.ContainsAny(u.Path, "\\\x00\r\n") {
		return errors.New("only local /api/ paths are allowed")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return errors.New("API path traversal is not allowed")
		}
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%25") || strings.Contains(u.Path, "//") {
		return errors.New("ambiguous API paths are not allowed")
	}
	return nil
}

func locateBackend() (backend.Options, error) { return locateBackendAt("") }

func locateBackendAt(explicitDataDir string) (backend.Options, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return backend.Options{}, err
	}
	options := backend.ResolvePaths(cwd)
	dataDir := explicitDataDir
	if dataDir == "" {
		dataDir = options.DataDir
	}
	if dataDir == "" {
		dataDir, err = backend.DefaultDataDir()
		if err != nil {
			return backend.Options{}, err
		}
	}
	if os.Getenv("ROUTER_FORGE_SERVER") == "" {
		resources, pathErr := mygo.App.Path(mygo.PathResources)
		if pathErr == nil {
			bundled := resourceOptions(resources, dataDir)
			if stat, e := os.Stat(bundled.ServerPath); e == nil && !stat.IsDir() {
				return bundled, nil
			}
		}
	}
	if options.ServerPath == "" {
		return backend.Options{}, errors.New("backend helpers not found")
	}
	options.DataDir = dataDir
	options.DevOrigin = "http://127.0.0.1:7790"
	return options, nil
}

func resourceOptions(resources, dataDir string) backend.Options {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	return backend.Options{
		DataDir:    dataDir,
		ServerPath: filepath.Join(resources, "server", "router-forge-server"+suffix),
		WorkerDir:  filepath.Join(resources, "workers"),
		WebDir:     filepath.Join(resources, "dist"),
		DevOrigin:  "http://127.0.0.1:7790",
	}
}

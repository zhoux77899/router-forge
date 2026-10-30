package executor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"golang.org/x/net/http/httpproxy"
)

const maxRegistryBytes = 32 << 20

type registryDataset struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Tasks   []registryTask `json:"tasks"`
}
type registryTask struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	GitURL    string `json:"git_url"`
	GitCommit string `json:"git_commit_id"`
}

func validateSuite(s domain.Suite) error {
	expected := domain.SWEBenchVerified()
	if s.ID != expected.ID || s.Dataset != expected.Dataset {
		return errors.New("only the native swe-bench/swe-bench-verified dataset is supported")
	}
	return nil
}

// ParseRegistry is a fixture/legacy parser, not the native dataset resolver.
// Its caller must select an explicit version; default selection belongs to Harbor.
func ParseRegistry(data []byte, suite domain.Suite) ([]domain.BenchmarkTask, error) {
	if len(data) > maxRegistryBytes {
		return nil, errors.New("Harbor registry exceeded its byte limit")
	}
	if suite.Dataset == "" || len(suite.Dataset) > 256 || !validText(suite.Dataset) || suite.Version == "" || len(suite.Version) > 256 || !validText(suite.Version) {
		return nil, errors.New("registry parsing requires an explicit dataset name and version")
	}
	var datasets []registryDataset
	if json.Unmarshal(data, &datasets) != nil || len(datasets) > 4096 {
		return nil, errors.New("invalid bounded Harbor registry")
	}
	var selected *registryDataset
	for i := range datasets {
		d := &datasets[i]
		if d.Name == suite.Dataset && d.Version == suite.Version {
			if selected != nil {
				return nil, errors.New("ambiguous dataset version in registry")
			}
			selected = d
		}
	}
	if selected == nil || len(selected.Tasks) == 0 || len(selected.Tasks) > 10000 {
		return nil, errors.New("requested dataset version is missing or has an invalid task count")
	}
	tasks := make([]domain.BenchmarkTask, 0, len(selected.Tasks))
	seen := map[string]bool{}
	for _, source := range selected.Tasks {
		task := domain.BenchmarkTask{Name: source.Name, Kind: "git", Path: source.Path, GitURL: source.GitURL, GitCommit: strings.ToLower(source.GitCommit)}
		if task.Validate() != nil || seen[task.Name] {
			return nil, errors.New("registry task identity is duplicate, unsafe or missing its full Git commit")
		}
		seen[task.Name] = true
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	return tasks, nil
}
func profileClient(e domain.Environment) (*http.Client, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if e.CACertPath != "" {
		if !validText(e.CACertPath) || strings.HasSuffix(strings.ToLower(e.CACertPath), ".env") {
			return nil, errors.New("invalid CA certificate path")
		}
		f, err := os.Open(e.CACertPath)
		if err != nil {
			return nil, errors.New("configured CA certificate is unavailable")
		}
		data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
		f.Close()
		if err != nil || len(data) > 1<<20 || !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("configured CA file is not a bounded PEM certificate bundle")
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	if e.ProxyURL != "" {
		proxy, err := url.Parse(e.ProxyURL)
		if err != nil || (proxy.Scheme != "http" && proxy.Scheme != "https") || proxy.Hostname() == "" || proxy.RawQuery != "" || proxy.Fragment != "" || !validText(e.ProxyURL) {
			return nil, errors.New("invalid HTTPS/HTTP proxy profile")
		}
		proxyFn := (&httpproxy.Config{HTTPProxy: e.ProxyURL, HTTPSProxy: e.ProxyURL, NoProxy: e.NoProxy}).ProxyFunc()
		tr.Proxy = func(r *http.Request) (*url.URL, error) { return proxyFn(r.URL) }
	}
	return &http.Client{Transport: tr, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" {
			return errors.New("registry redirect must remain HTTPS and bounded")
		}
		return nil
	}}, nil
}
func fetchPublic(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("invalid public metadata URL")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("public metadata fetch failed; verify proxy/CA/network configuration")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("public metadata endpoint returned a non-success status")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("public metadata response was incomplete or exceeded its limit")
	}
	return data, nil
}
func (w *nativeWorker) Resolve(ctx context.Context, e domain.Environment, s domain.Suite) (domain.ResolvedBenchmark, error) {
	if err := validateSuite(s); err != nil {
		return domain.ResolvedBenchmark{}, err
	}
	response, err := runNativeHarborBridge(ctx, e, "", nativeBridgeRequest{Operation: "resolve", Dataset: s.Dataset}, nil)
	if err != nil {
		return domain.ResolvedBenchmark{}, err
	}
	return normalizeNativeResolution(response.Resolution)
}

func (w *nativeWorker) Tasks(ctx context.Context, e domain.Environment, s domain.Suite) ([]domain.BenchmarkTask, error) {
	resolved, err := w.Resolve(ctx, e, s)
	return resolved.Tasks, err
}
func (w *nativeWorker) metadataClient(e domain.Environment) (*http.Client, error) {
	if w.clientFactory != nil {
		return w.clientFactory(e)
	}
	return profileClient(e)
}

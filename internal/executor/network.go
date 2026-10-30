package executor

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/netpolicy"
)

const containerNetworkRoot = "/etc/router-forge/network"

// nativeStageNetwork contains only credential-free configuration. A relay owns
// the private upstream credential; neither this contract nor Harbor receives it.
type nativeStageNetwork struct {
	Profile   domain.NetworkProfile `json:"profile"`
	Env       map[string]string     `json:"env"`
	HostEnv   map[string]string     `json:"host_env,omitempty"`
	Mounts    []HarborMount         `json:"mounts"`
	CASHA256  string                `json:"ca_sha256,omitempty"`
	directory string
	relay     *netpolicy.ProxyRelay
}
type harborExecutionNetwork struct {
	Source    *nativeStageNetwork `json:"source"`
	Build     *nativeStageNetwork `json:"build"`
	Agent     *nativeStageNetwork `json:"agent"`
	Verifier  *nativeStageNetwork `json:"verifier"`
	BuildArgs map[string]string   `json:"build_args"`
}

func networkPolicy(profile domain.NetworkProfile, credential *domain.ProxyCredential) netpolicy.Policy {
	policy := netpolicy.Policy{ProxyMode: string(profile.ProxyMode), ProxyURL: profile.ProxyURL, ProxyTargets: append([]string(nil), profile.ProxyTargets...), NoProxy: profile.NoProxy, TLSMode: string(profile.TLSMode), TrustMode: string(profile.TrustMode), CACertPath: profile.CACertPath, TLSRiskAccepted: profile.TLSRiskAccepted}
	if credential != nil {
		policy.Auth = &netpolicy.Credentials{Username: credential.Username, Password: credential.Password}
	}
	return policy
}
func sameNetworkSnapshot(left, right domain.NetworkSnapshot) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && string(a) == string(b)
}
func validateExecutionNetworks(spec domain.ExecutionSpec) error {
	if spec.Snapshot.Network == nil || spec.Networks == nil {
		return errors.New("execution requires the private frozen network contract")
	}
	if !sameNetworkSnapshot(*spec.Snapshot.Network, spec.Networks.Snapshot) {
		return errors.New("private execution network revisions differ from the approved snapshot")
	}
	allowed := map[string]bool{}
	for stage, profile := range spec.Snapshot.Network.ExecutionProfiles() {
		if stage == "source" && profile.ProxyAuth == domain.NetworkBasicAuth && profile.RelayMode != domain.NetworkLoopback {
			return errors.New("source proxy authentication requires an operation-scoped loopback relay")
		}
		if profile.ProxyAuth == domain.NetworkBasicAuth {
			allowed[profile.ID+"@"+profile.CredentialRevision] = true
			if _, err := spec.Networks.CredentialFor(profile); err != nil {
				return err
			}
		}
	}
	for key, credential := range spec.Networks.Credentials {
		if !allowed[key] || credential.Validate() != nil {
			return errors.New("private worker credentials must reference only approved execution profiles")
		}
	}
	return nil
}
func sourceNetwork(e domain.Environment) (domain.NetworkProfile, *domain.ProxyCredential, error) {
	if e.Networks == nil || e.Networks.SourceProfileID == "" || e.NetworkRuntime == nil {
		return domain.NetworkProfile{}, nil, errors.New("select an explicit source network profile before inspecting or resolving tasks")
	}
	n := e.NetworkRuntime.Snapshot
	if err := n.Source.ValidateSaved(); err != nil {
		return domain.NetworkProfile{}, nil, err
	}
	if n.Source.Scope != domain.NetworkEnvironment || n.Source.EnvironmentID != e.ID {
		return domain.NetworkProfile{}, nil, errors.New("operation network profile targets a different execution host")
	}
	if e.Networks.SourceProfileID != n.Source.ID {
		return domain.NetworkProfile{}, nil, errors.New("operation source binding differs from the private selected profile")
	}
	if n.Source.ProxyAuth == domain.NetworkBasicAuth && n.Source.RelayMode != domain.NetworkLoopback {
		return domain.NetworkProfile{}, nil, errors.New("source proxy authentication requires an operation-scoped loopback relay")
	}
	credential, err := e.NetworkRuntime.CredentialFor(n.Source)
	return n.Source, credential, err
}

func clearLegacyNetwork(e domain.Environment) domain.Environment {
	e.ProxyURL, e.NoProxy, e.CACertPath = "", "", ""
	e.SkipTLSVerification = false
	return e
}
func publicEnvironment(e domain.Environment) domain.Environment {
	e.NetworkRuntime = nil
	if e.Networks != nil {
		e = clearLegacyNetwork(e)
	}
	return e
}
func privateCommandNetworks(e domain.Environment) (*domain.ExecutionNetworks, error) {
	if e.NetworkRuntime == nil {
		return nil, nil
	}
	// Copy per command; a caller editing a profile or replacing credentials cannot
	// change an operation that is already queued on a reusable worker.
	data, err := json.Marshal(e.NetworkRuntime)
	if err != nil || len(data) > maxControlBody/2 {
		return nil, errors.New("private operation network contract exceeded its limit")
	}
	var copied domain.ExecutionNetworks
	if json.Unmarshal(data, &copied) != nil {
		return nil, errors.New("invalid private operation network contract")
	}
	allowed := map[string]bool{}
	for _, profile := range copied.Snapshot.ExecutionProfiles() {
		if profile.ProxyAuth == domain.NetworkBasicAuth {
			allowed[profile.ID+"@"+profile.CredentialRevision] = true
		}
	}
	for key := range copied.Credentials {
		if !allowed[key] {
			return nil, errors.New("model credentials cannot be sent to an execution worker")
		}
	}
	source := copied.Snapshot.Source
	selected := domain.ExecutionNetworks{Snapshot: domain.NetworkSnapshot{Source: source}, Credentials: map[string]domain.ProxyCredential{}}
	if source.ProxyAuth == domain.NetworkBasicAuth {
		credential, err := copied.CredentialFor(source)
		if err != nil {
			return nil, err
		}
		selected.Credentials[source.ID+"@"+source.CredentialRevision] = *credential
	}
	return &selected, nil
}

func networkProxyEnvironment(profile domain.NetworkProfile, proxyURL string, exclusions ...string) map[string]string {
	out := map[string]string{}
	if profile.ProxyMode == domain.NetworkProxy {
		for _, target := range profile.ProxyTargets {
			keys := []string{}
			switch target {
			case "http":
				keys = []string{"HTTP_PROXY", "http_proxy"}
			case "https":
				keys = []string{"HTTPS_PROXY", "https_proxy"}
			case "all":
				keys = []string{"ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"}
			}
			for _, key := range keys {
				out[key] = proxyURL
			}
		}
	}
	noProxy := strings.Trim(profile.NoProxy, ", ")
	for _, host := range exclusions {
		if host != "" {
			if noProxy != "" {
				noProxy += ","
			}
			noProxy += host
		}
	}
	out["NO_PROXY"], out["no_proxy"] = noProxy, noProxy
	return out
}
func stageDirectory(root, stage string, container bool) string {
	if container {
		return path.Join(containerNetworkRoot, stage)
	}
	if strings.HasPrefix(root, "/") {
		return path.Join(root, "network", stage)
	}
	return filepath.Join(root, "network", stage)
}
func stageNetworkConfig(profile domain.NetworkProfile, stage, root, proxyURL, gateway string, container bool) (*nativeStageNetwork, error) {
	if err := profile.ValidateSaved(); err != nil {
		return nil, err
	}
	if profile.ProxyAuth == domain.NetworkBasicAuth && proxyURL == "" {
		return nil, errors.New("authenticated network configuration requires an active owned relay")
	}
	if proxyURL == "" {
		proxyURL = profile.ProxyURL
	}
	directory := stageDirectory(root, stage, container)
	exclusions := []string{}
	if stage == "agent" {
		exclusions = []string{"router-forge-gateway", gateway, "localhost", "127.0.0.1"}
	}
	env := networkProxyEnvironment(profile, proxyURL, exclusions...)
	env["ROUTER_FORGE_NETWORK_STAGE"] = stage
	env["ROUTER_FORGE_NETWORK_BIN"] = path.Join(directory, "bin")
	if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
		env["ROUTER_FORGE_NETWORK_BIN"] = filepath.Join(directory, "bin")
	}
	for key, value := range map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "PIP_CONFIG_FILE": "/dev/null", "UV_NO_CONFIG": "1",
		"CURL_HOME": directory, "WGETRC": path.Join(directory, "wgetrc"),
		"NPM_CONFIG_USERCONFIG": path.Join(directory, "npmrc"), "NPM_CONFIG_GLOBALCONFIG": path.Join(directory, "npm-globalrc"),
		"npm_config_strict_ssl": "true",
	} {
		env[key] = value
	}
	if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
		env["GIT_CONFIG_GLOBAL"], env["PIP_CONFIG_FILE"] = os.DevNull, os.DevNull
		for key, file := range map[string]string{"WGETRC": "wgetrc", "NPM_CONFIG_USERCONFIG": "npmrc", "NPM_CONFIG_GLOBALCONFIG": "npm-globalrc"} {
			env[key] = filepath.Join(directory, file)
		}
	}
	if profile.TLSMode == domain.NetworkSkipTLS {
		if stage == "build" || stage == "verifier" {
			return nil, errors.New("original build and verifier TLS verification cannot be disabled")
		}
		for key, value := range map[string]string{"ROUTER_FORGE_SKIP_TLS_VERIFICATION": "1", "GIT_SSL_NO_VERIFY": "true", "NODE_TLS_REJECT_UNAUTHORIZED": "0", "npm_config_strict_ssl": "false", "UV_INSECURE_HOST": "*"} {
			env[key] = value
		}
		env["PYTHONPATH"] = path.Join(directory, "python")
		if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
			env["PYTHONPATH"] = filepath.Join(directory, "python")
		}
	} else if profile.TrustMode == domain.NetworkCustomCA {
		certificate := path.Join(directory, "ca.pem")
		if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
			certificate = filepath.Join(directory, "ca.pem")
		}
		for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "PIP_CERT", "npm_config_cafile"} {
			env[key] = certificate
		}
		env["UV_NATIVE_TLS"] = "true"
		env["PYTHONPATH"] = path.Join(directory, "python")
		if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
			env["PYTHONPATH"] = filepath.Join(directory, "python")
		}
	}
	if profile.Mirrors != nil {
		env["APT_CONFIG"] = path.Join(directory, "apt.conf")
		if profile.Mirrors.PyPI.Mode == domain.NetworkCustom {
			env["PIP_INDEX_URL"] = profile.Mirrors.PyPI.URL
		}
		if profile.Mirrors.NPM.Mode == domain.NetworkCustom {
			env["npm_config_registry"] = profile.Mirrors.NPM.URL
		}
		if profile.Mirrors.UV.Mode == domain.NetworkCustom {
			env["UV_INDEX_URL"] = profile.Mirrors.UV.URL
			env["UV_DEFAULT_INDEX"] = profile.Mirrors.UV.URL
		}
		if profile.Mirrors.UVPython.Mode == domain.NetworkCustom {
			env["UV_PYTHON_INSTALL_MIRROR"] = profile.Mirrors.UVPython.URL
		}
		if profile.Mirrors.Node.Mode == domain.NetworkCustom {
			for _, key := range []string{"NODEJS_ORG_MIRROR", "NVM_NODEJS_ORG_MIRROR", "N_NODE_MIRROR", "npm_config_disturl"} {
				env[key] = profile.Mirrors.Node.URL
			}
		}
	}
	env["PYTHONPATH"] = path.Join(directory, "python")
	if !container && runtime.GOOS == "windows" && !strings.HasPrefix(directory, "/") {
		env["PYTHONPATH"] = filepath.Join(directory, "python")
	}
	cfg := &nativeStageNetwork{Profile: profile, Env: env, Mounts: []HarborMount{}, directory: stageDirectory(root, stage, false)}
	if container {
		cfg.Mounts = append(cfg.Mounts, HarborMount{Type: "bind", Source: cfg.directory, Target: directory, ReadOnly: true})
		cfg.HostEnv = map[string]string{}
		for key, value := range env {
			if strings.HasPrefix(value, directory+"/") || value == directory {
				relative := strings.TrimPrefix(strings.TrimPrefix(value, directory), "/")
				if strings.HasPrefix(cfg.directory, "/") {
					value = path.Join(cfg.directory, relative)
				} else {
					value = filepath.Join(cfg.directory, filepath.FromSlash(relative))
				}
			}
			cfg.HostEnv[key] = value
		}
	}
	return cfg, nil
}

func frozenCA(filename string) ([]byte, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", errors.New("selected target-host CA file is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("selected target-host CA must be a regular PEM file")
	}
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(data) > 4<<20 || !x509.NewCertPool().AppendCertsFromPEM(data) {
		return nil, "", errors.New("selected target-host CA is not a bounded PEM bundle")
	}
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:]), nil
}
func writeStageNetwork(cfg *nativeStageNetwork, container bool) error {
	if err := privateDir(cfg.directory); err != nil {
		return errors.New("could not create owned stage network configuration")
	}
	files := map[string]string{"npmrc": "", "npm-globalrc": "", ".curlrc": "", "wgetrc": ""}
	if cfg.Profile.TLSMode == domain.NetworkSkipTLS {
		files[".curlrc"] = "insecure\nproxy-insecure\n"
		files["wgetrc"] = "check_certificate = off\n"
	} else if cfg.Profile.TrustMode == domain.NetworkCustomCA {
		data, digest, err := frozenCA(cfg.Profile.CACertPath)
		if err != nil {
			return err
		}
		cfg.CASHA256 = digest
		if err := writePrivate(filepath.Join(cfg.directory, "ca.pem"), data); err != nil {
			return errors.New("could not freeze selected stage certificate trust")
		}
		certificate := cfg.Env["SSL_CERT_FILE"]
		files[".curlrc"] = "cacert = \"" + certificate + "\"\n"
		files["wgetrc"] = "ca_certificate = " + certificate + "\ncheck_certificate = on\n"
	}
	if cfg.Profile.Mirrors != nil {
		files["apt.conf"] = aptNetworkConfiguration(cfg)
		if cfg.Profile.Mirrors.APT.Mode == domain.NetworkCustom {
			files["apt.list"] = cfg.Profile.Mirrors.APT.Sources + "\n"
		}
	}
	if err := privateDir(filepath.Join(cfg.directory, "python")); err != nil {
		return errors.New("could not create owned Python stage trust configuration")
	}
	files["python/router_forge_tls.py"] = nativeHarborTLS
	files["python/router_forge_network.py"] = nativeHarborNetwork
	files["python/sitecustomize.py"] = `import json,os
from pathlib import Path
try:
    import router_forge_network
    from router_forge_tls import apply_tls_policy
    router_forge_network.apply_tls_policy=apply_tls_policy
    router_forge_network.configure_process_network(json.loads(Path(__file__).with_name("process-network.json").read_text()))
except Exception:
    os._exit(78)
`
	contract, _ := json.Marshal(cfg)
	files["python/process-network.json"] = string(contract)
	for name, content := range files {
		if err := writePrivate(filepath.Join(cfg.directory, filepath.FromSlash(name)), []byte(content)); err != nil {
			return errors.New("could not persist owned stage client configuration")
		}
	}
	if err := writeNetworkClientWrappers(cfg, container); err != nil {
		return err
	}
	if container {
		// These files contain public settings only. The private attempt parent is
		// inaccessible from other host users; Docker mounts them read-only.
		err := filepath.WalkDir(cfg.directory, func(filename string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if entry.IsDir() || filepath.Dir(filename) == filepath.Join(cfg.directory, "bin") {
				mode = 0755
			}
			return os.Chmod(filename, mode)
		})
		if err != nil {
			return errors.New("could not expose credential-free readonly stage files to container users")
		}
	}
	return nil
}
func (cfg *nativeStageNetwork) close() {
	if cfg != nil && cfg.relay != nil {
		_ = cfg.relay.Close()
	}
}
func (cfg *harborExecutionNetwork) close() {
	if cfg == nil {
		return
	}
	for _, stage := range []*nativeStageNetwork{cfg.Source, cfg.Build, cfg.Agent, cfg.Verifier} {
		stage.close()
	}
}

func prepareStageNetwork(ctx context.Context, profile domain.NetworkProfile, credential *domain.ProxyCredential, stage, root, gateway string, tracker *dockerTracker) (_ *nativeStageNetwork, failure error) {
	container := stage == "agent" || stage == "verifier"
	cfg, err := stageNetworkConfig(profile, stage, root, profile.ProxyURL, gateway, container)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failure != nil {
			cfg.close()
			_ = os.RemoveAll(cfg.directory)
		}
	}()
	if err := writeStageNetwork(cfg, container); err != nil {
		return nil, err
	}
	if profile.ProxyAuth != domain.NetworkBasicAuth {
		if credential != nil {
			return nil, errors.New("unauthenticated stage configuration cannot carry proxy credentials")
		}
		return cfg, nil
	}
	if credential == nil || credential.Validate() != nil {
		return nil, errors.New("approved proxy credential reference is unavailable")
	}
	bind := net.ParseIP("127.0.0.1")
	authorize := func(ip net.IP) bool { return ip.Equal(bind) }
	if container {
		if profile.RelayMode != domain.NetworkDocker || tracker == nil {
			return nil, errors.New("container proxy authentication requires an owned-container relay capability")
		}
		if err := validateBindIP(gateway); err != nil {
			return nil, err
		}
		bind = net.ParseIP(gateway)
		authorize = func(ip net.IP) bool { return tracker.authorizeNetworkPeer(ctx, ip, stage, cfg.directory) }
	} else if profile.RelayMode != domain.NetworkLoopback {
		return nil, errors.New("source proxy authentication requires a selected loopback relay")
	}
	policy := networkPolicy(profile, credential)
	if profile.TLSMode == domain.NetworkVerifyTLS && profile.TrustMode == domain.NetworkCustomCA {
		policy.CACertPath = filepath.Join(cfg.directory, "ca.pem")
	}
	cfg.relay, err = netpolicy.StartProxyRelay(ctx, bind, policy, authorize)
	if err != nil {
		return nil, errors.New("could not create the selected private proxy relay")
	}
	for key, value := range networkProxyEnvironment(profile, cfg.relay.URL) {
		if !strings.EqualFold(key, "no_proxy") {
			cfg.Env[key] = value
			if cfg.HostEnv != nil {
				cfg.HostEnv[key] = value
			}
		}
	}
	if cfg.Profile.Mirrors != nil {
		if err := os.WriteFile(filepath.Join(cfg.directory, "apt.conf"), []byte(aptNetworkConfiguration(cfg)), 0644); err != nil {
			return nil, errors.New("could not finalize explicit APT relay configuration")
		}
	}
	contract, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(cfg.directory, "python", "process-network.json"), contract, 0600); err != nil {
		return nil, errors.New("could not finalize credential-free stage relay configuration")
	}
	if container {
		if err := os.Chmod(filepath.Join(cfg.directory, "python", "process-network.json"), 0644); err != nil {
			return nil, errors.New("could not expose readonly stage relay configuration")
		}
	}
	return cfg, nil
}
func staticExecutionNetwork(spec domain.ExecutionSpec, root, gateway string) (*harborExecutionNetwork, error) {
	n := spec.Snapshot.Network
	if n == nil {
		return nil, errors.New("explicit network profiles are required")
	}
	cfg := &harborExecutionNetwork{BuildArgs: networkProxyEnvironment(n.Build, n.Build.ProxyURL)}
	var err error
	for _, stage := range []string{"source", "build", "agent", "verifier"} {
		profile := n.ExecutionProfiles()[stage]
		value, e := stageNetworkConfig(profile, stage, root, "", gateway, stage == "agent" || stage == "verifier")
		if e != nil {
			err = e
			break
		}
		switch stage {
		case "source":
			cfg.Source = value
		case "build":
			cfg.Build = value
		case "agent":
			cfg.Agent = value
		case "verifier":
			cfg.Verifier = value
		}
	}
	return cfg, err
}
func prepareExecutionNetwork(ctx context.Context, spec domain.ExecutionSpec, root, gateway string, tracker *dockerTracker) (_ *harborExecutionNetwork, failure error) {
	defer func() {
		if failure != nil {
			_ = os.RemoveAll(filepath.Join(root, "network"))
		}
	}()
	cfg := &harborExecutionNetwork{BuildArgs: networkProxyEnvironment(spec.Snapshot.Network.Build, spec.Snapshot.Network.Build.ProxyURL)}
	for _, stage := range []string{"source", "build", "agent", "verifier"} {
		profile := spec.Snapshot.Network.ExecutionProfiles()[stage]
		credential, err := spec.Networks.CredentialFor(profile)
		if err != nil {
			cfg.close()
			return nil, err
		}
		var value *nativeStageNetwork
		if stage == "build" {
			value, err = stageNetworkConfig(profile, stage, root, "", gateway, false)
		} else {
			value, err = prepareStageNetwork(ctx, profile, credential, stage, root, gateway, tracker)
		}
		if err != nil {
			cfg.close()
			return nil, err
		}
		switch stage {
		case "source":
			cfg.Source = value
		case "build":
			cfg.Build = value
		case "agent":
			cfg.Agent = value
		case "verifier":
			cfg.Verifier = value
		}
	}
	return cfg, nil
}
func stageProcessEnvironment(e domain.Environment, root, token string, stage *nativeStageNetwork) []string {
	base := netpolicy.StripNetworkEnvironment(nativeProcessEnv(clearLegacyNetwork(e), root, token))
	keys := make([]string, 0, len(stage.Env))
	for key := range stage.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		base = append(base, key+"="+stage.Env[key])
	}
	if info, err := os.Stat(filepath.Join(stage.directory, "bin")); err == nil && info.IsDir() {
		for i, entry := range base {
			key, value, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(key, "PATH") {
				base[i] = key + "=" + filepath.Join(stage.directory, "bin") + string(os.PathListSeparator) + value
				break
			}
		}
	}
	return base
}
func prepareSourceOperation(ctx context.Context, e domain.Environment) (domain.Environment, *nativeStageNetwork, []string, func(), error) {
	profile, credential, err := sourceNetwork(e)
	if err != nil {
		return e, nil, nil, func() {}, err
	}
	work, err := selectedWorkDir(e)
	if err != nil {
		return e, nil, nil, func() {}, err
	}
	root := filepath.Join(work, "operations", domain.NewID("network"))
	if err := privateDir(root); err != nil {
		return e, nil, nil, func() {}, errors.New("could not reserve an owned network operation directory")
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	for _, name := range []string{"home", "scratch"} {
		if err := privateDir(filepath.Join(root, name)); err != nil {
			cleanup()
			return e, nil, nil, func() {}, err
		}
	}
	stage, err := prepareStageNetwork(ctx, profile, credential, "source", root, "", nil)
	if err != nil {
		cleanup()
		return e, nil, nil, func() {}, err
	}
	close := func() { stage.close(); cleanup() }
	e = clearLegacyNetwork(e)
	return e, stage, stageProcessEnvironment(e, root, "", stage), close, nil
}

func (w *nativeWorker) CheckNetwork(ctx context.Context, e domain.Environment, operation domain.NetworkOperation) (domain.NetworkCheck, error) {
	profile := operation.Profile
	result := domain.NetworkCheck{ProfileID: profile.ID, ProfileRevision: profile.Revision, TargetURL: operation.TargetURL, ObservedAt: domain.Now(), Issues: []string{}}
	if err := profile.ValidateSaved(); err != nil {
		return result, err
	}
	if profile.Scope != domain.NetworkEnvironment || profile.EnvironmentID != e.ID {
		return result, errors.New("network check must run on the explicitly selected execution host")
	}
	if !domain.ValidNetworkURL(operation.TargetURL, false) {
		return result, errors.New("network check requires an explicit credential-free HTTP(S) target URL")
	}
	if profile.ProxyAuth == domain.NetworkBasicAuth {
		if operation.Credential == nil || operation.Credential.Validate() != nil {
			return result, errors.New("selected proxy credential is unavailable")
		}
	} else if operation.Credential != nil {
		return result, errors.New("direct or unauthenticated checks cannot carry proxy credentials")
	}
	policy := networkPolicy(profile, operation.Credential)
	if profile.TLSMode == domain.NetworkVerifyTLS && profile.TrustMode == domain.NetworkCustomCA {
		// Freeze the file for both this request and its evidence, not two separate
		// reads of a target-host file that could change during the check.
		data, digest, err := frozenCA(profile.CACertPath)
		if err != nil {
			return result, err
		}
		work, err := selectedWorkDir(e)
		if err != nil {
			return result, err
		}
		root := filepath.Join(work, "operations", domain.NewID("check"))
		if err := privateDir(root); err != nil {
			return result, errors.New("could not reserve private certificate check configuration")
		}
		defer os.RemoveAll(root)
		policy.CACertPath = filepath.Join(root, "ca.pem")
		if err := writePrivate(policy.CACertPath, data); err != nil {
			return result, errors.New("could not freeze certificate check configuration")
		}
		result.CASHA256 = digest
	}
	client, err := netpolicy.NewClient(policy)
	if err != nil {
		return result, err
	}
	defer client.CloseIdleConnections()
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(bounded, http.MethodHead, operation.TargetURL, nil)
	response, err := client.Do(request)
	result.ObservedAt = domain.Now()
	if err != nil {
		result.Issues = append(result.Issues, "selected host HTTP request failed; this is not a Docker daemon, image pull or container check")
		return result, nil
	}
	defer response.Body.Close()
	result.Reachable = true
	status := response.StatusCode
	result.HTTPStatus = &status
	if response.TLS != nil {
		verified := profile.TLSMode == domain.NetworkVerifyTLS && len(response.TLS.VerifiedChains) > 0
		result.TLSVerified = &verified
	}
	result.Issues = append(result.Issues, "host HTTP observation only; container, daemon and image-pull connectivity are not inferred")
	return result, nil
}

func redactObservedURL(value string) string {
	if value == "" {
		return ""
	}
	if len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return "[redacted]"
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" {
		return "[redacted]"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

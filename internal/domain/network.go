package domain

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type NetworkScope string
type NetworkRoute string
type NetworkTLSMode string
type NetworkTrustMode string
type NetworkSourceMode string
type NetworkProxyAuth string
type NetworkRelayMode string

const (
	NetworkDesktop     NetworkScope      = "desktop"
	NetworkEnvironment NetworkScope      = "environment"
	NetworkDirect      NetworkRoute      = "direct"
	NetworkProxy       NetworkRoute      = "proxy"
	NetworkVerifyTLS   NetworkTLSMode    = "verify"
	NetworkSkipTLS     NetworkTLSMode    = "skip"
	NetworkSystemCA    NetworkTrustMode  = "system"
	NetworkCustomCA    NetworkTrustMode  = "custom"
	NetworkNative      NetworkSourceMode = "native"
	NetworkCustom      NetworkSourceMode = "custom"
	NetworkNoAuth      NetworkProxyAuth  = "none"
	NetworkBasicAuth   NetworkProxyAuth  = "basic"
	NetworkLoopback    NetworkRelayMode  = "loopback"
	NetworkDocker      NetworkRelayMode  = "docker"
)

type NetworkSource struct {
	Mode    NetworkSourceMode `json:"mode"`
	URL     string            `json:"url,omitempty"`
	Sources string            `json:"sources,omitempty"`
}

type NetworkMirrors struct {
	APT      NetworkSource `json:"apt"`
	PyPI     NetworkSource `json:"pypi"`
	NPM      NetworkSource `json:"npm"`
	Node     NetworkSource `json:"node"`
	UV       NetworkSource `json:"uv"`
	UVPython NetworkSource `json:"uv_python"`
}

func (m NetworkMirrors) Sources() map[string]NetworkSource {
	return map[string]NetworkSource{"apt": m.APT, "pypi": m.PyPI, "npm": m.NPM, "node": m.Node, "uv": m.UV, "uv_python": m.UVPython}
}

func (m NetworkMirrors) Native() bool {
	for _, source := range m.Sources() {
		if source.Mode != NetworkNative {
			return false
		}
	}
	return true
}

type NetworkProfile struct {
	ID                 string           `json:"id"`
	Revision           string           `json:"revision"`
	Name               string           `json:"name"`
	Scope              NetworkScope     `json:"scope"`
	EnvironmentID      string           `json:"environment_id,omitempty"`
	ProxyMode          NetworkRoute     `json:"proxy_mode"`
	ProxyURL           string           `json:"proxy_url,omitempty"`
	ProxyTargets       []string         `json:"proxy_targets,omitempty"`
	ProxyAuth          NetworkProxyAuth `json:"proxy_auth,omitempty"`
	NoProxy            string           `json:"no_proxy,omitempty"`
	RelayMode          NetworkRelayMode `json:"relay_mode,omitempty"`
	TLSMode            NetworkTLSMode   `json:"tls_mode"`
	TrustMode          NetworkTrustMode `json:"trust_mode"`
	CACertPath         string           `json:"ca_cert_path,omitempty"`
	Mirrors            *NetworkMirrors  `json:"mirrors,omitempty"`
	HasCredentials     bool             `json:"has_credentials"`
	CredentialRevision string           `json:"credential_revision,omitempty"`
	TLSRiskAccepted    bool             `json:"tls_risk_accepted"`
	CreatedAt          string           `json:"created_at"`
}

type NetworkProfileInput struct {
	Profile          NetworkProfile `json:"profile"`
	ProxyUsername    *string        `json:"proxy_username,omitempty"`
	ProxyPassword    *string        `json:"proxy_password,omitempty"`
	ClearCredentials bool           `json:"clear_credentials,omitempty"`
	ConfirmTLSRisk   bool           `json:"confirm_tls_risk,omitempty"`
}

type EnvironmentNetworks struct {
	SourceProfileID   string `json:"source_profile_id"`
	BuildProfileID    string `json:"build_profile_id"`
	AgentProfileID    string `json:"agent_profile_id"`
	VerifierProfileID string `json:"verifier_profile_id"`
}

func (n EnvironmentNetworks) Complete() bool {
	return networkID(n.SourceProfileID) && networkID(n.BuildProfileID) && networkID(n.AgentProfileID) && networkID(n.VerifierProfileID)
}

type NetworkSnapshot struct {
	Source   NetworkProfile `json:"source"`
	Build    NetworkProfile `json:"build"`
	Agent    NetworkProfile `json:"agent"`
	Verifier NetworkProfile `json:"verifier"`
	Model    NetworkProfile `json:"model"`
}

func (n NetworkSnapshot) ExecutionProfiles() map[string]NetworkProfile {
	return map[string]NetworkProfile{"source": n.Source, "build": n.Build, "agent": n.Agent, "verifier": n.Verifier}
}
func (n NetworkSnapshot) ExecutionTLSRisk() bool {
	for _, profile := range n.ExecutionProfiles() {
		if profile.TLSMode == NetworkSkipTLS {
			return true
		}
	}
	return false
}
func (n NetworkSnapshot) ModelTLSRisk() bool { return n.Model.TLSMode == NetworkSkipTLS }

// ProxyCredential is carried only in the private parent/worker channel, never a public snapshot.
type ProxyCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c ProxyCredential) Validate() error {
	if c.Username == "" || c.Password == "" || len(c.Username) > 4096 || len(c.Password) > 16384 || !utf8.ValidString(c.Username+c.Password) || strings.Contains(c.Username, ":") || strings.IndexFunc(c.Username+c.Password, unicode.IsControl) >= 0 {
		return errors.New("invalid proxy credential")
	}
	return nil
}

type ExecutionNetworks struct {
	Snapshot    NetworkSnapshot            `json:"snapshot"`
	Credentials map[string]ProxyCredential `json:"credentials,omitempty"`
}

type NetworkOperation struct {
	Profile    NetworkProfile   `json:"profile"`
	Credential *ProxyCredential `json:"credential,omitempty"`
	TargetURL  string           `json:"target_url"`
}

func (n ExecutionNetworks) CredentialFor(p NetworkProfile) (*ProxyCredential, error) {
	if p.ProxyAuth != NetworkBasicAuth {
		return nil, nil
	}
	credential, ok := n.Credentials[p.ID+"@"+p.CredentialRevision]
	if !ok || credential.Validate() != nil {
		return nil, errors.New("approved proxy credential reference is unavailable")
	}
	return &credential, nil
}

type NetworkCheckInput struct {
	ProfileRevision string `json:"profile_revision"`
	TargetURL       string `json:"target_url"`
	ConfirmTLSRisk  bool   `json:"confirm_tls_risk,omitempty"`
}

type NetworkCheck struct {
	ProfileID       string   `json:"profile_id"`
	ProfileRevision string   `json:"profile_revision"`
	TargetURL       string   `json:"target_url"`
	Reachable       bool     `json:"reachable"`
	ObservedAt      string   `json:"observed_at"`
	HTTPStatus      *int     `json:"http_status,omitempty"`
	TLSVerified     *bool    `json:"tls_verified,omitempty"`
	CASHA256        string   `json:"ca_sha256,omitempty"`
	Issues          []string `json:"issues"`
}

type DockerNetworkObservation struct {
	Mirrors    []string `json:"mirrors"`
	HTTPProxy  string   `json:"http_proxy,omitempty"`
	HTTPSProxy string   `json:"https_proxy,omitempty"`
	NoProxy    string   `json:"no_proxy,omitempty"`
	ObservedAt string   `json:"observed_at"`
}

type StartInput struct {
	ConfirmPaid             bool   `json:"confirm_paid"`
	ConfigHash              string `json:"config_hash"`
	ConfirmExecutionTLSRisk bool   `json:"confirm_execution_tls_risk,omitempty"`
	ConfirmModelTLSRisk     bool   `json:"confirm_model_tls_risk,omitempty"`
}

var networkIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func networkID(value string) bool { return networkIDPattern.MatchString(value) }

func ValidNetworkURL(value string, proxy bool) bool {
	if !boundedText(value, 4096) || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	return !proxy || u.Path == "" || u.Path == "/"
}

func validateAPTSources(value string) error {
	if value == "" || len(value) > 65536 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return errors.New("custom apt source requires a bounded .list document")
	}
	entries := 0
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "deb" && fields[0] != "deb-src" {
			return errors.New("apt source must use deb or deb-src .list syntax")
		}
		index := 1
		if strings.HasPrefix(fields[index], "[") {
			for index < len(fields) && !strings.HasSuffix(fields[index], "]") {
				index++
			}
			if index >= len(fields) {
				return errors.New("invalid apt source options")
			}
			options := strings.ToLower(strings.Join(fields[1:index+1], " "))
			if strings.Contains(options, "trusted=yes") || strings.Contains(options, "allow-insecure=yes") || strings.Contains(options, "allow-unauthenticated") {
				return errors.New("network mirrors cannot disable apt repository authentication")
			}
			index++
		}
		if len(fields)-index < 3 || !ValidNetworkURL(fields[index], false) {
			return errors.New("apt source requires a credential-free HTTP(S) URI, suite and components")
		}
		entries++
	}
	if entries == 0 {
		return errors.New("custom apt source has no repository entries")
	}
	return nil
}

func (p NetworkProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 256 || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return errors.New("network profile name is required")
	}
	if p.Scope != NetworkDesktop && p.Scope != NetworkEnvironment {
		return errors.New("network host scope must be explicitly selected")
	}
	if p.Scope == NetworkEnvironment && !networkID(p.EnvironmentID) || p.Scope == NetworkDesktop && p.EnvironmentID != "" {
		return errors.New("network profile must target the selected host, not another host's paths or network")
	}
	if p.ProxyMode != NetworkDirect && p.ProxyMode != NetworkProxy {
		return errors.New("direct or proxy mode must be explicitly selected")
	}
	if p.ProxyMode == NetworkProxy {
		if !ValidNetworkURL(p.ProxyURL, true) {
			return errors.New("proxy requires a credential-free HTTP(S) origin")
		}
		if p.ProxyAuth != NetworkNoAuth && p.ProxyAuth != NetworkBasicAuth {
			return errors.New("proxy authentication mode must be explicitly selected")
		}
		seen := map[string]bool{}
		for _, target := range p.ProxyTargets {
			if target != "http" && target != "https" && target != "all" || seen[target] {
				return errors.New("proxy targets must be unique HTTP, HTTPS or ALL selections")
			}
			seen[target] = true
		}
		if len(seen) == 0 {
			return errors.New("proxy targets must be explicitly selected")
		}
		if p.ProxyAuth == NetworkBasicAuth && (p.RelayMode != NetworkLoopback && p.RelayMode != NetworkDocker || p.Scope == NetworkDesktop && p.RelayMode != NetworkLoopback) {
			return errors.New("authenticated proxy relay scope must match the selected host")
		}
		if p.ProxyAuth == NetworkNoAuth && p.RelayMode != "" {
			return errors.New("unauthenticated proxy does not use a credential relay")
		}
	} else if p.ProxyURL != "" || len(p.ProxyTargets) != 0 || p.ProxyAuth != "" || p.RelayMode != "" {
		return errors.New("direct mode cannot retain active proxy settings")
	}
	if len(p.NoProxy) > 8192 || strings.ContainsAny(p.NoProxy+p.CACertPath, "\x00\r\n\t") {
		return errors.New("network fields contain invalid control characters")
	}
	if p.TLSMode != NetworkVerifyTLS && p.TLSMode != NetworkSkipTLS {
		return errors.New("TLS verification mode must be explicitly selected")
	}
	if p.TrustMode != NetworkSystemCA && p.TrustMode != NetworkCustomCA {
		return errors.New("certificate trust mode must be explicitly selected")
	}
	if p.TLSMode == NetworkSkipTLS && !p.TLSRiskAccepted {
		return errors.New("skipping TLS verification requires independent risk confirmation")
	}
	if p.TLSMode == NetworkVerifyTLS && p.TrustMode == NetworkCustomCA && strings.TrimSpace(p.CACertPath) == "" {
		return errors.New("custom certificate trust requires a target-host CA path")
	}
	if len(p.CACertPath) > 4096 {
		return errors.New("CA path is too long")
	}
	if p.Scope == NetworkEnvironment {
		if p.Mirrors == nil {
			return errors.New("execution download sources must be explicitly selected")
		}
		for name, source := range p.Mirrors.Sources() {
			if source.Mode != NetworkNative && source.Mode != NetworkCustom {
				return errors.New("every applicable package download source must be explicitly selected")
			}
			if source.Mode == NetworkNative && (source.URL != "" || source.Sources != "") {
				return errors.New("native download sources cannot retain custom overrides")
			}
			if source.Mode == NetworkCustom {
				if name == "apt" {
					if source.URL != "" {
						return errors.New("apt mirrors require explicit .list sources, not an inferred distribution")
					}
					if err := validateAPTSources(source.Sources); err != nil {
						return err
					}
				} else if source.Sources != "" || !ValidNetworkURL(source.URL, false) {
					return errors.New("package mirror requires a credential-free HTTP(S) URL")
				}
			}
		}
	} else if p.Mirrors != nil {
		return errors.New("model gateway profiles cannot apply Linux package settings")
	}
	return nil
}

func (p NetworkProfile) ValidateSaved() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !networkID(p.ID) || !networkID(p.Revision) {
		return errors.New("network profile identity and revision are required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
		return errors.New("network profile creation time is required")
	}
	if p.ProxyAuth == NetworkBasicAuth && (!p.HasCredentials || !networkID(p.CredentialRevision)) {
		return errors.New("authenticated proxy credential reference is unavailable")
	}
	return nil
}

func (n NetworkSnapshot) Validate(environmentID string) error {
	seen := map[string]string{}
	profiles := n.ExecutionProfiles()
	profiles["model"] = n.Model
	for stage, profile := range profiles {
		if err := profile.ValidateSaved(); err != nil {
			return err
		}
		if stage == "model" {
			if profile.Scope != NetworkDesktop {
				return errors.New("model HTTPS network must target the Desktop host")
			}
		} else if profile.Scope != NetworkEnvironment || profile.EnvironmentID != environmentID {
			return errors.New("execution network must target the approved execution host")
		}
		data, _ := json.Marshal(profile)
		if old, exists := seen[profile.ID]; exists && old != string(data) {
			return errors.New("one approved profile cannot contain mixed revisions or settings")
		}
		seen[profile.ID] = string(data)
		if (stage == "agent" || stage == "verifier") && profile.ProxyAuth == NetworkBasicAuth && profile.RelayMode != NetworkDocker {
			return errors.New("container proxy authentication requires an explicitly selected owned-container relay")
		}
	}
	if n.Build.TLSMode != NetworkVerifyTLS || n.Build.TrustMode != NetworkSystemCA || n.Build.ProxyAuth == NetworkBasicAuth || n.Build.Mirrors == nil || !n.Build.Mirrors.Native() {
		return errors.New("original Dockerfile builds support explicit unauthenticated proxy arguments only; CA, insecure TLS, package overrides and authenticated build relays are not supported")
	}
	if n.Verifier.TLSMode != NetworkVerifyTLS {
		return errors.New("original verifier certificate checks cannot be relaxed; select a strict verifier profile")
	}
	return nil
}

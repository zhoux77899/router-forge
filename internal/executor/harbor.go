package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

const attemptKeyName = "ROUTER_FORGE_ATTEMPT_TOKEN"

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type HarborJobConfig struct {
	JobName               string                  `json:"job_name"`
	JobsDir               string                  `json:"jobs_dir"`
	NAttempts             int                     `json:"n_attempts"`
	InstallOnly           bool                    `json:"install_only"`
	SkipCredentialChecks  bool                    `json:"skip_credential_checks"`
	TimeoutMultiplier     float64                 `json:"timeout_multiplier"`
	Debug                 bool                    `json:"debug"`
	NConcurrentTrials     int                     `json:"n_concurrent_trials"`
	Quiet                 bool                    `json:"quiet"`
	Retry                 HarborRetryConfig       `json:"retry"`
	Environment           HarborEnvironmentConfig `json:"environment"`
	Verifier              HarborVerifierConfig    `json:"verifier"`
	Metrics               []any                   `json:"metrics"`
	Agents                []HarborAgentConfig     `json:"agents"`
	Datasets              []any                   `json:"datasets"`
	Tasks                 []HarborTaskConfig      `json:"tasks"`
	Artifacts             []any                   `json:"artifacts"`
	ExtraInstructionPaths []string                `json:"extra_instruction_paths"`
	ExtraInstructions     []string                `json:"extra_instructions"`
	SourceJobs            []any                   `json:"source_jobs"`
}
type HarborRetryConfig struct {
	MaxRetries        int      `json:"max_retries"`
	IncludeExceptions []string `json:"include_exceptions"`
	ExcludeExceptions []string `json:"exclude_exceptions"`
	WaitMultiplier    float64  `json:"wait_multiplier"`
	MinWaitSec        float64  `json:"min_wait_sec"`
	MaxWaitSec        float64  `json:"max_wait_sec"`
}
type HarborAgentConfig struct {
	Name                    string                     `json:"name"`
	ModelName               string                     `json:"model_name"`
	BaseURL                 string                     `json:"base_url"`
	ModelAPIKeyName         string                     `json:"model_api_key_name"`
	ModelAPIFormat          string                     `json:"model_api_format"`
	SkipModelParsing        bool                       `json:"skip_model_parsing"`
	NConcurrent             int                        `json:"n_concurrent"`
	OverrideTimeoutSec      int                        `json:"override_timeout_sec"`
	OverrideSetupTimeoutSec int                        `json:"override_setup_timeout_sec"`
	MaxTimeoutSec           int                        `json:"max_timeout_sec"`
	ResumeTrajectory        bool                       `json:"resume_trajectory"`
	Skills                  []string                   `json:"skills"`
	ExtraAllowedHosts       []string                   `json:"extra_allowed_hosts"`
	Kwargs                  map[string]json.RawMessage `json:"kwargs"`
	Env                     map[string]string          `json:"env"`
	MCPServers              []any                      `json:"mcp_servers"`
}
type HarborTaskConfig struct {
	Path        string `json:"path,omitempty"`
	GitURL      string `json:"git_url,omitempty"`
	GitCommitID string `json:"git_commit_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Source      string `json:"source,omitempty"`
	Overwrite   bool   `json:"overwrite"`
	DownloadDir string `json:"download_dir"`
}
type HarborMount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}
type HarborEnvironmentConfig struct {
	Type                    string            `json:"type"`
	ForceBuild              bool              `json:"force_build"`
	Delete                  bool              `json:"delete"`
	Stream                  bool              `json:"stream"`
	CPUEnforcementPolicy    string            `json:"cpu_enforcement_policy"`
	MemoryEnforcementPolicy string            `json:"memory_enforcement_policy"`
	Mounts                  []HarborMount     `json:"mounts"`
	ExtraDockerCompose      []string          `json:"extra_docker_compose"`
	Env                     map[string]string `json:"env"`
	Kwargs                  map[string]any    `json:"kwargs"`
	ExtraAllowedHosts       []string          `json:"extra_allowed_hosts"`
}
type HarborVerifierConfig struct {
	Disable           bool              `json:"disable"`
	Env               map[string]string `json:"env"`
	Kwargs            map[string]any    `json:"kwargs"`
	ExtraAllowedHosts []string          `json:"extra_allowed_hosts"`
}

func harborAPIFormat(p domain.Protocol) string {
	switch p {
	case domain.ChatCompletions:
		return "openai_chat_completions"
	case domain.Responses:
		return "openai_responses"
	case domain.AnthropicMessages:
		return "anthropic_messages"
	}
	return ""
}
func ValidateExecutionSpec(spec domain.ExecutionSpec) error {
	run := spec.Snapshot.Spec
	if err := run.Validate(); err != nil {
		return err
	}
	if !run.ConfirmPaid {
		return errors.New("execution requires separate explicit paid-run confirmation")
	}
	for _, id := range []string{spec.RunID, spec.TaskID, spec.AttemptID, spec.JobName} {
		if !simpleName.MatchString(id) {
			return errors.New("run, task, attempt and job identifiers must be bounded safe names")
		}
	}
	if err := spec.Snapshot.ValidateSources(); err != nil {
		return err
	}
	if !validText(spec.GatewayToken) || len(spec.GatewayToken) < 8 || len(spec.GatewayToken) > 512 || strings.ContainsAny(spec.GatewayToken, " \t") {
		return errors.New("bounded attempt lease token is required")
	}
	snapshot := spec.Snapshot
	if snapshot.Environment.ID != run.EnvironmentID || snapshot.Model.ID != run.ModelID || snapshot.Model.ProviderID != snapshot.Provider.ID || snapshot.Agent.Name != run.Agent || !snapshot.Model.Protocol.Valid() || !validText(snapshot.Model.Name) || len(snapshot.Model.Name) > 256 {
		return errors.New("execution snapshot model, provider, environment and agent identities are inconsistent")
	}
	if !snapshot.Agent.HarborSupported || !containsProtocol(snapshot.Agent.SupportedProtocols, snapshot.Model.Protocol) {
		return errors.New("selected agent does not support the approved native model protocol")
	}
	if snapshot.Agent.Command == "" {
		return errors.New("selected Harbor adapter has no supported native CLI version observation command")
	}
	return validateAgentKwargs(run.AgentKwargs, snapshot.Agent.Schema)
}
func validateAgentKwargs(kwargs map[string]json.RawMessage, schema json.RawMessage) error {
	if len(kwargs) > 32 {
		return errors.New("too many agent options")
	}
	var options optionsSchema
	if json.Unmarshal(schema, &options) != nil {
		return errors.New("recorded agent option schema is invalid")
	}
	for name, raw := range kwargs {
		lowered := strings.ToLower(name)
		for _, blocked := range []string{"api", "key", "auth", "credential", "base", "endpoint", "url", "model", "version", "config", "prompt", "env", "mcp", "skill", "system", "path", "resume", "trajectory", "token"} {
			if strings.Contains(lowered, blocked) {
				return fmt.Errorf("agent option %s cannot override execution routing, credentials or task behavior", name)
			}
		}
		if !simpleName.MatchString(name) || len(raw) > 1024 || options.Properties[name] == nil {
			return errors.New("agent option is not in the recorded native schema")
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return errors.New("invalid agent option JSON")
		}
		switch value.(type) {
		case nil, string, float64, bool:
		default:
			return errors.New("first-release agent options must be native scalar values")
		}
		if !scalarMatchesSchema(value, options.Properties[name]) {
			return errors.New("agent option does not match its recorded native schema")
		}
	}
	return nil
}
func scalarMatchesSchema(value any, raw json.RawMessage) bool {
	var s struct {
		Type  string            `json:"type"`
		AnyOf []json.RawMessage `json:"anyOf"`
		Enum  []any             `json:"enum"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	if len(s.AnyOf) > 0 {
		for _, branch := range s.AnyOf {
			if scalarMatchesSchema(value, branch) {
				return true
			}
		}
		return false
	}
	valid := false
	switch v := value.(type) {
	case nil:
		valid = s.Type == "null"
	case string:
		valid = s.Type == "string"
	case bool:
		valid = s.Type == "boolean"
	case float64:
		valid = s.Type == "number" || s.Type == "integer" && v == float64(int64(v))
	}
	if !valid {
		return false
	}
	if len(s.Enum) > 0 {
		for _, entry := range s.Enum {
			a, _ := json.Marshal(entry)
			b, _ := json.Marshal(value)
			if string(a) == string(b) {
				return true
			}
		}
		return false
	}
	return true
}
func profileEnv(e domain.Environment, relayHost, relayIP string, container bool) map[string]string {
	out := map[string]string{}
	if e.ProxyURL != "" {
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			out[key] = e.ProxyURL
		}
	}
	noProxy := strings.Trim(e.NoProxy, ", ")
	for _, host := range []string{relayHost, relayIP, "localhost", "127.0.0.1"} {
		if host != "" {
			if noProxy != "" {
				noProxy += ","
			}
			noProxy += host
		}
	}
	out["NO_PROXY"] = noProxy
	out["no_proxy"] = noProxy
	if e.CACertPath != "" {
		certificate := e.CACertPath
		if container {
			certificate = "/etc/router-forge/ca.pem"
		}
		for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "UV_NATIVE_TLS"} {
			if key == "UV_NATIVE_TLS" {
				out[key] = "true"
			} else {
				out[key] = certificate
			}
		}
	}
	return out
}
func BuildHarborJobConfig(spec domain.ExecutionSpec, task domain.BenchmarkTask, relayRoot, jobsDir, composePath string) (HarborJobConfig, error) {
	if err := ValidateExecutionSpec(spec); err != nil {
		return HarborJobConfig{}, err
	}
	if err := task.Validate(); err != nil {
		return HarborJobConfig{}, err
	}
	if task != spec.Snapshot.ResolvedTasks[0] {
		return HarborJobConfig{}, errors.New("task reference differs from the exact source approved in the draft")
	}
	endpoint, err := url.Parse(relayRoot)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "router-forge-gateway" || endpoint.Port() == "" || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.User != nil {
		return HarborJobConfig{}, errors.New("agent relay must use a dynamic worker-side Docker-reachable address")
	}
	if !validText(jobsDir) || !validText(composePath) {
		return HarborJobConfig{}, errors.New("invalid private Harbor paths")
	}
	run := spec.Snapshot.Spec
	agent := spec.Snapshot.Agent
	kwargs := map[string]json.RawMessage{}
	for key, value := range run.AgentKwargs {
		kwargs[key] = append(json.RawMessage(nil), value...)
	}
	gateway := spec.Snapshot.Environment.DockerHostGateway
	if spec.Snapshot.Runtime != nil && spec.Snapshot.Runtime.DockerGateway != "" {
		gateway = spec.Snapshot.Runtime.DockerGateway
	}
	env := profileEnv(spec.Snapshot.Environment, endpoint.Hostname(), gateway, true)
	mounts := []HarborMount{}
	if spec.Snapshot.Environment.CACertPath != "" {
		mounts = append(mounts, HarborMount{Type: "bind", Source: spec.Snapshot.Environment.CACertPath, Target: "/etc/router-forge/ca.pem", ReadOnly: true})
	}
	downloadDir := filepath.Join(filepath.Dir(jobsDir), "task-cache")
	if strings.HasPrefix(jobsDir, "/") {
		downloadDir = path.Join(path.Dir(jobsDir), "task-cache")
	}
	reference := HarborTaskConfig{Overwrite: false, DownloadDir: downloadDir, Source: spec.Snapshot.Suite.Dataset}
	if task.Kind == "package" {
		reference.Name, reference.Ref = task.PackageName, task.Ref
	} else {
		reference.Path, reference.GitURL, reference.GitCommitID = task.Path, task.GitURL, task.GitCommit
	}
	modelSelector := spec.Snapshot.Model.Name
	baseURL := relayBaseURL(relayRoot, spec.Snapshot.Model.Protocol)
	if agent.Name == "opencode" {
		// The native provider selector preserves the approved upstream model ID.
		modelSelector = "router-forge/" + modelSelector
		baseURL = relayRoot + "/v1"
	}
	cfg := HarborJobConfig{
		JobName: spec.JobName, JobsDir: jobsDir, NAttempts: 1, InstallOnly: false, SkipCredentialChecks: false, TimeoutMultiplier: 1, NConcurrentTrials: 1, Quiet: true,
		Retry:       HarborRetryConfig{MaxRetries: 0, ExcludeExceptions: []string{}, WaitMultiplier: 1, MinWaitSec: 1, MaxWaitSec: 60},
		Environment: HarborEnvironmentConfig{Type: "docker", Delete: true, CPUEnforcementPolicy: "auto", MemoryEnforcementPolicy: "auto", Mounts: mounts, ExtraDockerCompose: []string{composePath}, Env: env, Kwargs: map[string]any{}, ExtraAllowedHosts: []string{}},
		Verifier:    HarborVerifierConfig{Disable: false, Env: env, Kwargs: map[string]any{}, ExtraAllowedHosts: []string{}}, Metrics: []any{},
		Agents:   []HarborAgentConfig{{Name: agent.Name, ModelName: modelSelector, BaseURL: baseURL, ModelAPIKeyName: attemptKeyName, ModelAPIFormat: harborAPIFormat(spec.Snapshot.Model.Protocol), SkipModelParsing: true, NConcurrent: 1, OverrideTimeoutSec: run.TimeoutSec, OverrideSetupTimeoutSec: run.SetupTimeoutSec, MaxTimeoutSec: run.TimeoutSec, Skills: []string{}, ExtraAllowedHosts: []string{endpoint.Hostname()}, Kwargs: kwargs, Env: env, MCPServers: []any{}}},
		Datasets: []any{}, Tasks: []HarborTaskConfig{reference},
		Artifacts: []any{}, ExtraInstructionPaths: []string{}, ExtraInstructions: []string{}, SourceJobs: []any{},
	}
	// Harbor's separate-verifier factory retains kwargs, but drops compose overlays.
	if len(mounts) > 0 {
		cfg.Environment.Kwargs["mounts"] = mounts
	}
	return cfg, nil
}

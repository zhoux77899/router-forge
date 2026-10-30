package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const Version = "0.1.0"

const AgentVersionPattern = `^v?(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*|\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`

var agentVersion = regexp.MustCompile(AgentVersionPattern)

func ValidAgentVersion(version string) bool { return agentVersion.MatchString(version) }

type Protocol string

const (
	ChatCompletions   Protocol = "chat_completions"
	Responses         Protocol = "responses"
	AnthropicMessages Protocol = "anthropic_messages"
)

func (p Protocol) Valid() bool {
	return p == ChatCompletions || p == Responses || p == AnthropicMessages
}
func (p Protocol) APIFormat() string {
	switch p {
	case ChatCompletions:
		return "chat_completions"
	case Responses:
		return "responses"
	case AnthropicMessages:
		return "anthropic"
	}
	return ""
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Environment struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Type              string            `json:"type"`
	Distribution      string            `json:"distribution,omitempty"`
	Host              string            `json:"host,omitempty"`
	User              string            `json:"user,omitempty"`
	Port              int               `json:"port,omitempty"`
	IdentityFile      string            `json:"identity_file,omitempty"`
	KnownHostsFile    string            `json:"known_hosts_file,omitempty"`
	WorkDir           string            `json:"work_dir,omitempty"`
	WorkerOS          string            `json:"worker_os,omitempty"`
	WorkerArch        string            `json:"worker_arch,omitempty"`
	Tools             map[string]string `json:"tools,omitempty"`
	ProxyURL          string            `json:"proxy_url,omitempty"`
	NoProxy           string            `json:"no_proxy,omitempty"`
	CACertPath        string            `json:"ca_cert_path,omitempty"`
	DockerHostGateway string            `json:"docker_host_gateway,omitempty"`
	CreatedAt         string            `json:"created_at"`
}

type ToolStatus struct {
	Name          string `json:"name"`
	Available     bool   `json:"available"`
	Path          string `json:"path,omitempty"`
	Version       string `json:"version,omitempty"`
	VersionOutput string `json:"version_output,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type Agent struct {
	Name               string          `json:"name"`
	Command            string          `json:"command"`
	Path               string          `json:"path,omitempty"`
	Version            string          `json:"version,omitempty"`
	Installed          bool            `json:"installed"`
	HarborSupported    bool            `json:"harbor_supported"`
	SupportedProtocols []Protocol      `json:"supported_protocols"`
	SupportsVersion    bool            `json:"supports_version"`
	ContainerInstall   string          `json:"container_install"`
	Reason             string          `json:"reason,omitempty"`
	Schema             json.RawMessage `json:"schema,omitempty"`
}

type Inspection struct {
	EnvironmentID string       `json:"environment_id"`
	OS            string       `json:"os"`
	Arch          string       `json:"arch"`
	HomeDir       string       `json:"home_dir"`
	WorkDir       string       `json:"work_dir"`
	Ready         bool         `json:"ready"`
	Tools         []ToolStatus `json:"tools"`
	Agents        []Agent      `json:"agents"`
	Issues        []string     `json:"issues"`
	DockerGateway string       `json:"docker_gateway,omitempty"`
}

type Provider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	HasKey      bool   `json:"has_key"`
	KeyHint     string `json:"key_hint,omitempty"`
	KeyRevision string `json:"key_revision,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type ProviderInput struct {
	ID      string  `json:"id,omitempty"`
	Name    string  `json:"name"`
	BaseURL string  `json:"base_url"`
	APIKey  *string `json:"api_key,omitempty"`
}

type Pricing struct {
	Currency             string  `json:"currency"`
	InputPerMillion      string  `json:"input_per_million"`
	OutputPerMillion     string  `json:"output_per_million"`
	CacheReadPerMillion  *string `json:"cache_read_per_million,omitempty"`
	CacheWritePerMillion *string `json:"cache_write_per_million,omitempty"`
	Source               string  `json:"source"`
}

type Model struct {
	ID          string   `json:"id"`
	ProviderID  string   `json:"provider_id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Protocol    Protocol `json:"protocol"`
	Pricing     *Pricing `json:"pricing,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

type Suite struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Dataset        string `json:"dataset"`
	Version        string `json:"version"`
	RegistryURL    string `json:"registry_url"`
	TaskRepository string `json:"task_repository"`
	TaskCommit     string `json:"task_commit"`
	TaskCount      int    `json:"task_count"`
	SuggestedTask  string `json:"suggested_task"`
}

func SWEBenchVerified() Suite {
	return Suite{ID: "swebench-verified", Name: "SWE-bench Verified", Dataset: "swe-bench/swe-bench-verified"}
}

type BenchmarkTask struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Path        string `json:"path,omitempty"`
	GitURL      string `json:"git_url,omitempty"`
	GitCommit   string `json:"git_commit,omitempty"`
	PackageName string `json:"package_name,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Revision    string `json:"revision,omitempty"`
}

type RunSpec struct {
	EnvironmentID   string                     `json:"environment_id"`
	ModelID         string                     `json:"model_id"`
	SuiteID         string                     `json:"suite_id"`
	TaskNames       []string                   `json:"task_names"`
	Agent           string                     `json:"agent"`
	AgentVersion    string                     `json:"agent_version,omitempty"`
	AgentKwargs     map[string]json.RawMessage `json:"agent_kwargs,omitempty"`
	TimeoutSec      int                        `json:"timeout_sec"`
	SetupTimeoutSec int                        `json:"setup_timeout_sec"`
	MaxRequests     int                        `json:"max_requests"`
	MaxOutputTokens int                        `json:"max_output_tokens"`
	Concurrency     int                        `json:"concurrency"`
	Attempts        int                        `json:"attempts"`
	ConfirmPaid     bool                       `json:"confirm_paid"`
}

func (s RunSpec) Validate() error {
	if s.EnvironmentID == "" || s.ModelID == "" || s.Agent == "" {
		return fmt.Errorf("environment, model and agent are required")
	}
	if s.AgentVersion != "" {
		return fmt.Errorf("new runs use Harbor's default container agent installation; explicit agent versions are legacy-only")
	}
	if s.SuiteID != "swebench-verified" {
		return fmt.Errorf("unsupported suite")
	}
	if len(s.TaskNames) != 1 || !ValidTaskName(s.TaskNames[0]) {
		return fmt.Errorf("exactly one safe Harbor task name is required")
	}
	if s.Concurrency != 1 || s.Attempts != 1 {
		return fmt.Errorf("concurrency and attempts must both be 1")
	}
	if s.TimeoutSec < 1 || s.TimeoutSec > 86400 || s.SetupTimeoutSec < 1 || s.SetupTimeoutSec > 86400 {
		return fmt.Errorf("timeouts must be between 1 and 86400 seconds")
	}
	if s.MaxRequests < 1 || s.MaxRequests > 10000 || s.MaxOutputTokens < 1 || s.MaxOutputTokens > 1000000 {
		return fmt.Errorf("positive bounded request and output limits are required")
	}
	return nil
}

type RunSnapshot struct {
	Spec                RunSpec          `json:"spec"`
	Environment         Environment      `json:"environment"`
	Model               Model            `json:"model"`
	Provider            Provider         `json:"provider"`
	Suite               Suite            `json:"suite"`
	HarborVersion       string           `json:"harbor_version"`
	UVVersion           string           `json:"uv_version,omitempty"`
	Agent               Agent            `json:"agent"`
	ConfigHash          string           `json:"config_hash"`
	FormatVersion       int              `json:"format_version,omitempty"`
	RegistryKind        string           `json:"registry_kind,omitempty"`
	RegistryMetadataURL string           `json:"registry_metadata_url,omitempty"`
	DatasetDigest       string           `json:"dataset_digest,omitempty"`
	DatasetRevision     string           `json:"dataset_revision,omitempty"`
	ResolvedAt          string           `json:"resolved_at,omitempty"`
	ResolutionHash      string           `json:"resolution_hash,omitempty"`
	ResolvedTasks       []BenchmarkTask  `json:"resolved_tasks,omitempty"`
	PreflightTools      []ToolStatus     `json:"preflight_tools,omitempty"`
	Runtime             *RuntimeSnapshot `json:"runtime,omitempty"`
}

type Run struct {
	ID         string         `json:"id"`
	State      string         `json:"state"`
	CreatedAt  string         `json:"created_at"`
	StartedAt  *string        `json:"started_at"`
	FinishedAt *string        `json:"finished_at"`
	Snapshot   RunSnapshot    `json:"snapshot"`
	Error      string         `json:"error,omitempty"`
	Tasks      []Task         `json:"tasks"`
	Summary    RequestSummary `json:"summary"`
}

type Task struct {
	ID       string    `json:"id"`
	RunID    string    `json:"run_id"`
	Name     string    `json:"name"`
	State    string    `json:"state"`
	Attempts []Attempt `json:"attempts"`
}

type Attempt struct {
	ID            string          `json:"id"`
	TaskID        string          `json:"task_id"`
	RunID         string          `json:"run_id"`
	State         string          `json:"state"`
	JobName       string          `json:"job_name"`
	TrialName     string          `json:"trial_name,omitempty"`
	StartedAt     *string         `json:"started_at"`
	FinishedAt    *string         `json:"finished_at"`
	Reward        *float64        `json:"reward"`
	ExceptionType string          `json:"exception_type,omitempty"`
	Error         string          `json:"error,omitempty"`
	ExitCode      *int            `json:"exit_code"`
	ResultPath    string          `json:"result_path,omitempty"`
	ImageDigests  []string        `json:"image_digests"`
	AgentVersion  string          `json:"agent_version,omitempty"`
	HarborMetrics json.RawMessage `json:"harbor_metrics,omitempty"`
}

type JobEvent struct {
	ID        int64  `json:"id"`
	RunID     string `json:"run_id"`
	AttemptID string `json:"attempt_id,omitempty"`
	Timestamp string `json:"timestamp"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
}

type Usage struct {
	InputTokens      *int64 `json:"input_tokens"`
	OutputTokens     *int64 `json:"output_tokens"`
	CacheReadTokens  *int64 `json:"cache_read_tokens"`
	CacheWriteTokens *int64 `json:"cache_write_tokens"`
	ReasoningTokens  *int64 `json:"reasoning_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
	Source           string `json:"source"`
	Completeness     string `json:"completeness"`
}

type RequestRecord struct {
	ID                 string   `json:"id"`
	RunID              string   `json:"run_id"`
	TaskID             string   `json:"task_id"`
	AttemptID          string   `json:"attempt_id"`
	ProviderID         string   `json:"provider_id"`
	ModelID            string   `json:"model_id"`
	Protocol           Protocol `json:"protocol"`
	Kind               string   `json:"kind"`
	RequestedModel     string   `json:"requested_model"`
	ActualModel        string   `json:"actual_model"`
	UpstreamRequestID  string   `json:"upstream_request_id,omitempty"`
	UpstreamResponseID string   `json:"upstream_response_id,omitempty"`
	StartedAt          string   `json:"started_at"`
	FinishedAt         *string  `json:"finished_at"`
	DurationMS         *int64   `json:"duration_ms"`
	FirstByteMS        *int64   `json:"first_byte_ms"`
	HTTPStatus         *int     `json:"http_status"`
	State              string   `json:"state"`
	ErrorClass         string   `json:"error_class,omitempty"`
	Usage              Usage    `json:"usage"`
	CostAmount         *string  `json:"cost_amount"`
	Currency           *string  `json:"currency"`
	CostSource         string   `json:"cost_source"`
}

type CurrencyTotal struct {
	Currency  string `json:"currency"`
	Amount    string `json:"amount"`
	Estimated bool   `json:"estimated"`
}
type RequestSummary struct {
	RequestCount      int             `json:"request_count"`
	InferenceCount    int             `json:"inference_count"`
	UnknownUsageCount int             `json:"unknown_usage_count"`
	UnknownCostCount  int             `json:"unknown_cost_count"`
	InputTokens       int64           `json:"input_tokens"`
	OutputTokens      int64           `json:"output_tokens"`
	CacheReadTokens   int64           `json:"cache_read_tokens"`
	CacheWriteTokens  int64           `json:"cache_write_tokens"`
	ReasoningTokens   int64           `json:"reasoning_tokens"`
	Costs             []CurrencyTotal `json:"costs"`
}

type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

type BackendInfo struct {
	URL          string `json:"url"`
	ControlToken string `json:"control_token"`
	PID          int    `json:"pid"`
	Version      string `json:"version"`
	DataDir      string `json:"data_dir"`
	WorkerDir    string `json:"worker_dir"`
}

type ExecutionSpec struct {
	RunID        string      `json:"run_id"`
	TaskID       string      `json:"task_id"`
	AttemptID    string      `json:"attempt_id"`
	JobName      string      `json:"job_name"`
	Snapshot     RunSnapshot `json:"snapshot"`
	GatewayToken string      `json:"gateway_token"`
}

type WorkerEvent struct {
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}
type ExecutionResult struct {
	State         string          `json:"state"`
	TrialName     string          `json:"trial_name"`
	Reward        *float64        `json:"reward"`
	ExceptionType string          `json:"exception_type"`
	Error         string          `json:"error"`
	ExitCode      int             `json:"exit_code"`
	ResultPath    string          `json:"result_path"`
	ImageDigests  []string        `json:"image_digests"`
	AgentVersion  string          `json:"agent_version"`
	HarborMetrics json.RawMessage `json:"harbor_metrics,omitempty"`
}

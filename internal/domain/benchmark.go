package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const SnapshotFormatVersion = 2

var taskNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}(?:/[A-Za-z0-9][A-Za-z0-9_.-]{0,127})?$`)
var commitPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidTaskName(name string) bool { return taskNamePattern.MatchString(name) }
func ValidCommit(commit string) bool { return commitPattern.MatchString(commit) }
func ValidDigest(digest string) bool { return digestPattern.MatchString(digest) }
func ValidHash(hash string) bool     { return hashPattern.MatchString(hash) }

func boundedText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n\t")
}

func ValidSourceURL(value string) bool {
	if !boundedText(value, 2048) {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(u.Path, "\x00\r\n\t")
}

func (t BenchmarkTask) Validate() error {
	if !ValidTaskName(t.Name) {
		return errors.New("invalid canonical Harbor task name")
	}
	switch t.Kind {
	case "git":
		if !ValidSourceURL(t.GitURL) || !ValidCommit(t.GitCommit) || !boundedText(t.Path, 2048) || strings.Contains(t.Path, "\\") || strings.HasPrefix(t.Path, "/") || strings.Contains(t.Path, ":") || path.Clean(t.Path) != t.Path || t.Path == "." || strings.HasPrefix(t.Path, "../") || t.Name != path.Base(t.Path) || t.PackageName != "" || t.Ref != "" || t.Revision != "" {
			return errors.New("Git task requires a safe relative path, HTTPS repository and recorded full commit")
		}
	case "package":
		if !strings.Contains(t.PackageName, "/") || !ValidTaskName(t.PackageName) || t.Name != t.PackageName || !ValidDigest(t.Ref) || t.Path != "" || t.GitURL != "" || t.GitCommit != "" || t.Revision != "" && !boundedText(t.Revision, 128) {
			return errors.New("package task requires its canonical name and resolved SHA-256 digest, without Git fields")
		}
	default:
		return errors.New("task source must be Git or Harbor package")
	}
	return nil
}

type ResolvedBenchmark struct {
	Suite               Suite           `json:"suite"`
	RegistryKind        string          `json:"registry_kind"`
	RegistryMetadataURL string          `json:"registry_metadata_url,omitempty"`
	DatasetDigest       string          `json:"dataset_digest,omitempty"`
	DatasetRevision     string          `json:"dataset_revision,omitempty"`
	ResolvedAt          string          `json:"resolved_at"`
	ResolutionHash      string          `json:"resolution_hash"`
	Tasks               []BenchmarkTask `json:"tasks"`
}

func (r ResolvedBenchmark) Validate() error {
	expected := SWEBenchVerified()
	if r.Suite.ID != expected.ID || r.Suite.Dataset != expected.Dataset || !boundedText(r.Suite.Version, 256) || !ValidSourceURL(r.Suite.RegistryURL) || r.Suite.TaskCount < 1 || r.Suite.TaskCount != len(r.Tasks) || len(r.Tasks) > 10000 {
		return errors.New("resolved benchmark identity, version, registry or task count is invalid")
	}
	if r.RegistryKind != "json" && r.RegistryKind != "harbor" && r.RegistryKind != "package" || r.RegistryMetadataURL != "" && !ValidSourceURL(r.RegistryMetadataURL) {
		return errors.New("invalid recorded registry source")
	}
	if r.DatasetDigest != "" && !ValidDigest(r.DatasetDigest) || r.RegistryKind == "package" && (!ValidDigest(r.DatasetDigest) || r.Suite.Version != r.DatasetDigest) || r.DatasetRevision != "" && !boundedText(r.DatasetRevision, 128) {
		return errors.New("invalid resolved dataset digest or revision")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.ResolvedAt); err != nil {
		return errors.New("benchmark resolution time must be recorded")
	}
	seen := map[string]bool{}
	for _, task := range r.Tasks {
		if err := task.Validate(); err != nil {
			return err
		}
		if seen[task.Name] {
			return errors.New("ambiguous or duplicate canonical task name")
		}
		seen[task.Name] = true
	}
	return nil
}

func (r ResolvedBenchmark) Hash() (string, error) {
	r.ResolvedAt, r.ResolutionHash = "", ""
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type RuntimeSnapshot struct {
	Tools              []ToolStatus `json:"tools"`
	ObservedAt         string       `json:"observed_at"`
	WorkDir            string       `json:"work_dir,omitempty"`
	DockerGateway      string       `json:"docker_gateway,omitempty"`
	RegistryURL        string       `json:"registry_url,omitempty"`
	TaskPath           string       `json:"task_path,omitempty"`
	TaskName           string       `json:"task_name,omitempty"`
	TrialPrefix        string       `json:"trial_prefix,omitempty"`
	AgentVersion       string       `json:"agent_version,omitempty"`
	AgentVersionOutput string       `json:"agent_version_output,omitempty"`
	AgentObservedAt    string       `json:"agent_observed_at,omitempty"`
}

type ExecutionObservation struct {
	Kind               string         `json:"kind"`
	Timestamp          string         `json:"timestamp"`
	Task               *BenchmarkTask `json:"task,omitempty"`
	Tools              []ToolStatus   `json:"tools,omitempty"`
	WorkDir            string         `json:"work_dir,omitempty"`
	DockerGateway      string         `json:"docker_gateway,omitempty"`
	RegistryURL        string         `json:"registry_url,omitempty"`
	TaskPath           string         `json:"task_path,omitempty"`
	TaskName           string         `json:"task_name,omitempty"`
	TrialPrefix        string         `json:"trial_prefix,omitempty"`
	AgentVersion       string         `json:"agent_version,omitempty"`
	AgentVersionOutput string         `json:"agent_version_output,omitempty"`
}

func (o ExecutionObservation) Validate() error {
	if _, err := time.Parse(time.RFC3339Nano, o.Timestamp); err != nil {
		return errors.New("execution observation time is required")
	}
	switch o.Kind {
	case "tools":
		if o.Task != nil || o.RegistryURL != "" || o.TaskPath != "" || o.TaskName != "" || o.TrialPrefix != "" || o.AgentVersion != "" || o.AgentVersionOutput != "" || o.DockerGateway != "" && !boundedText(o.DockerGateway, 256) {
			return errors.New("tool observation contains unrelated or invalid runtime fields")
		}
		if len(o.Tools) < 2 || len(o.Tools) > 3 || !boundedText(o.WorkDir, 2048) {
			return errors.New("actual tool and work-directory observations are required")
		}
		seen := map[string]bool{}
		for _, tool := range o.Tools {
			if seen[tool.Name] || tool.Name != "uv" && tool.Name != "harbor" && tool.Name != "docker" || tool.Available && !boundedText(tool.Path, 2048) || tool.Path != "" && !boundedText(tool.Path, 2048) || len(tool.Version) > 256 || strings.ContainsAny(tool.Version, "\x00\r\n\t") || len(tool.VersionOutput) > 32768 || strings.ContainsRune(tool.VersionOutput, 0) || !utf8.ValidString(tool.VersionOutput) {
				return errors.New("invalid actual tool observation")
			}
			seen[tool.Name] = true
		}
		if !seen["uv"] || !seen["harbor"] {
			return errors.New("uv and Harbor observations must both be recorded")
		}
	case "task":
		if o.Task == nil || o.Task.Validate() != nil {
			return errors.New("task observation must include its exact approved source reference")
		}
		if len(o.Tools) != 0 || o.WorkDir != "" || o.DockerGateway != "" || o.AgentVersion != "" || o.AgentVersionOutput != "" || !ValidSourceURL(o.RegistryURL) || !boundedText(o.TaskPath, 2048) || !ValidTaskName(o.TaskName) || !ValidTaskName(o.TrialPrefix) || strings.Contains(o.TrialPrefix, "/") {
			return errors.New("actual task materialization source and identity must be recorded without unrelated fields")
		}
	case "agent":
		if o.Task != nil || len(o.Tools) != 0 || o.WorkDir != "" || o.DockerGateway != "" || o.RegistryURL != "" || o.TaskPath != "" || o.TaskName != "" || o.TrialPrefix != "" || !boundedText(o.AgentVersion, 256) || strings.TrimSpace(o.AgentVersionOutput) == "" || len(o.AgentVersionOutput) > 32768 || !utf8.ValidString(o.AgentVersionOutput) || strings.ContainsRune(o.AgentVersionOutput, 0) {
			return errors.New("independently observed container agent version and raw output are required without unrelated fields")
		}
	default:
		return errors.New("unknown execution observation kind")
	}
	return nil
}

func (s RunSnapshot) ApprovalHash() (string, error) {
	spec := s.Spec
	spec.ConfirmPaid = false
	approved := struct {
		FormatVersion       int             `json:"format_version"`
		Spec                RunSpec         `json:"spec"`
		Environment         Environment     `json:"environment"`
		Model               Model           `json:"model"`
		Provider            Provider        `json:"provider"`
		Suite               Suite           `json:"suite"`
		RegistryKind        string          `json:"registry_kind"`
		RegistryMetadataURL string          `json:"registry_metadata_url"`
		DatasetDigest       string          `json:"dataset_digest"`
		DatasetRevision     string          `json:"dataset_revision"`
		ResolutionHash      string          `json:"resolution_hash"`
		ResolvedTasks       []BenchmarkTask `json:"resolved_tasks"`
	}{s.FormatVersion, spec, s.Environment, s.Model, s.Provider, s.Suite, s.RegistryKind, s.RegistryMetadataURL, s.DatasetDigest, s.DatasetRevision, s.ResolutionHash, s.ResolvedTasks}
	b, err := json.Marshal(approved)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s RunSnapshot) ValidateSources() error {
	if s.FormatVersion != SnapshotFormatVersion {
		return errors.New("this legacy draft must be recreated before execution")
	}
	if err := s.Spec.Validate(); err != nil {
		return err
	}
	expected := SWEBenchVerified()
	if s.Suite.ID != expected.ID || s.Suite.Dataset != expected.Dataset || !boundedText(s.Suite.Version, 256) || !ValidSourceURL(s.Suite.RegistryURL) || s.Suite.TaskCount < 1 || len(s.ResolvedTasks) != 1 || s.ResolvedTasks[0].Name != s.Spec.TaskNames[0] || !ValidHash(s.ResolutionHash) {
		return errors.New("run requires the recorded benchmark version and exact selected task source")
	}
	if s.RegistryKind != "json" && s.RegistryKind != "harbor" && s.RegistryKind != "package" || s.RegistryMetadataURL != "" && !ValidSourceURL(s.RegistryMetadataURL) || s.DatasetDigest != "" && !ValidDigest(s.DatasetDigest) || s.RegistryKind == "package" && s.Suite.Version != s.DatasetDigest {
		return errors.New("run registry source or dataset digest is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, s.ResolvedAt); err != nil {
		return errors.New("run dataset resolution time is not recorded")
	}
	if err := s.ResolvedTasks[0].Validate(); err != nil {
		return err
	}
	hash, err := s.ApprovalHash()
	if err != nil || hash != s.ConfigHash {
		return errors.New("run's approved configuration hash is inconsistent")
	}
	return nil
}

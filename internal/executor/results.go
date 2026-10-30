package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

var ErrIncompleteResult = errors.New("Harbor result.json is missing, partial, or not yet complete")
var ErrResultIdentity = errors.New("Harbor result identity differs from the approved single-task source")

type harborTrial struct {
	TaskName   string `json:"task_name"`
	TrialName  string `json:"trial_name"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	AgentInfo  struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agent_info"`
	AgentResult    map[string]json.RawMessage `json:"agent_result"`
	VerifierResult *struct {
		Rewards map[string]float64 `json:"rewards"`
	} `json:"verifier_result"`
	ExceptionInfo *struct {
		Type string `json:"exception_type"`
	} `json:"exception_info"`
	EnvironmentSetup json.RawMessage `json:"environment_setup"`
	AgentSetup       json.RawMessage `json:"agent_setup"`
	AgentExecution   json.RawMessage `json:"agent_execution"`
	Verifier         json.RawMessage `json:"verifier"`
}

func ParseHarborResult(data []byte, resultPath string, exitCode int) (domain.ExecutionResult, error) {
	result := domain.ExecutionResult{ExitCode: exitCode, ResultPath: resultPath, ImageDigests: []string{}}
	var trial harborTrial
	if len(data) > 2<<20 || json.Unmarshal(data, &trial) != nil || trial.TaskName == "" || trial.FinishedAt == "" {
		return result, ErrIncompleteResult
	}
	if !domain.ValidTaskName(trial.TaskName) || !simpleName.MatchString(trial.TrialName) {
		return result, ErrResultIdentity
	}
	result.TrialName = trial.TrialName
	if trial.VerifierResult != nil {
		if reward, ok := trial.VerifierResult.Rewards["reward"]; ok {
			if math.IsNaN(reward) || math.IsInf(reward, 0) {
				return result, errors.New("verifier reward is not finite")
			}
			result.Reward = &reward
		}
	}
	if trial.ExceptionInfo != nil {
		if !simpleName.MatchString(trial.ExceptionInfo.Type) {
			return result, errors.New("invalid Harbor exception type")
		}
		result.ExceptionType = trial.ExceptionInfo.Type
		result.Error = "Harbor reported " + result.ExceptionType
	}
	switch {
	case strings.Contains(result.ExceptionType, "Timeout"):
		result.State = "timed_out"
	case result.ExceptionType == "CancelledError" || result.ExceptionType == "CancellationError":
		result.State = "cancelled"
	case strings.HasPrefix(result.ExceptionType, "Agent") && !strings.Contains(result.ExceptionType, "Setup"):
		result.State = "agent_error"
	case result.ExceptionType != "":
		result.State = "infrastructure_error"
	case exitCode != 0:
		result.State = "cli_error"
		result.Error = fmt.Sprintf("Harbor CLI exited with code %d; verifier reward is independent", exitCode)
	case result.Reward == nil:
		result.State = "infrastructure_error"
		result.Error = "Harbor finished without an original verifier reward"
	default:
		result.State = "completed"
	}
	metrics := map[string]any{}
	for _, key := range []string{"n_input_tokens", "n_cache_tokens", "n_output_tokens", "cost_usd"} {
		if raw, ok := trial.AgentResult[key]; ok {
			var value *float64
			if json.Unmarshal(raw, &value) == nil && (value == nil || *value >= 0 && !math.IsInf(*value, 0) && !math.IsNaN(*value)) {
				metrics[key] = value
			}
		}
	}
	if trial.AgentInfo.Version != "" && len(trial.AgentInfo.Version) <= 128 {
		metrics["harbor_reported_agent_version"] = trial.AgentInfo.Version
	}
	if trial.StartedAt != "" {
		metrics["started_at"] = trial.StartedAt
	}
	metrics["finished_at"] = trial.FinishedAt
	for key, raw := range map[string]json.RawMessage{"environment_setup": trial.EnvironmentSetup, "agent_setup": trial.AgentSetup, "agent_execution": trial.AgentExecution, "verifier": trial.Verifier} {
		var timing struct {
			StartedAt  *string `json:"started_at"`
			FinishedAt *string `json:"finished_at"`
		}
		if len(raw) > 0 && json.Unmarshal(raw, &timing) == nil {
			metrics[key] = map[string]*string{"started_at": timing.StartedAt, "finished_at": timing.FinishedAt}
		}
	}
	result.HarborMetrics, _ = json.Marshal(metrics)
	return result, nil
}
func readResultFile(filename string) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, ErrIncompleteResult
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, ErrResultIdentity
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, ErrIncompleteResult
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		return nil, ErrIncompleteResult
	}
	return data, nil
}
func readHarborResultOnce(jobDir, task, trialPrefix string, exitCode int, sources ...domain.BenchmarkTask) (domain.ExecutionResult, error) {
	entries, err := os.ReadDir(jobDir)
	if err != nil {
		return domain.ExecutionResult{}, ErrIncompleteResult
	}
	if len(entries) > 1024 {
		return domain.ExecutionResult{}, ErrResultIdentity
	}
	var candidate string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		filename := filepath.Join(jobDir, entry.Name(), "result.json")
		if _, err := os.Lstat(filename); err != nil {
			continue
		}
		if !simpleName.MatchString(entry.Name()) || !strings.HasPrefix(entry.Name(), trialPrefix+"__") {
			return domain.ExecutionResult{}, ErrResultIdentity
		}
		if candidate != "" {
			return domain.ExecutionResult{}, ErrResultIdentity
		}
		candidate = filename
	}
	if candidate != "" {
		data, err := readResultFile(candidate)
		if err != nil {
			return domain.ExecutionResult{}, err
		}
		var identity struct {
			TaskName  string `json:"task_name"`
			TrialName string `json:"trial_name"`
		}
		if json.Unmarshal(data, &identity) == nil && (identity.TaskName != "" && identity.TaskName != task || identity.TrialName != "" && identity.TrialName != filepath.Base(filepath.Dir(candidate))) {
			return domain.ExecutionResult{}, ErrResultIdentity
		}
		if len(sources) > 0 {
			if err := verifyHarborTaskSource(data, sources[0]); err != nil {
				return domain.ExecutionResult{}, err
			}
		}
		return ParseHarborResult(data, candidate, exitCode)
	}
	filename := filepath.Join(jobDir, "result.json")
	data, err := readResultFile(filename)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	var job struct {
		Total      int               `json:"n_total_trials"`
		FinishedAt string            `json:"finished_at"`
		Trials     []json.RawMessage `json:"trial_results"`
	}
	if json.Unmarshal(data, &job) != nil || job.FinishedAt == "" {
		return domain.ExecutionResult{}, ErrIncompleteResult
	}
	if job.Total != 1 || len(job.Trials) > 1 {
		return domain.ExecutionResult{}, ErrResultIdentity
	}
	if len(job.Trials) == 1 {
		var identity struct {
			TaskName  string `json:"task_name"`
			TrialName string `json:"trial_name"`
		}
		if json.Unmarshal(job.Trials[0], &identity) != nil || identity.TaskName != task || !strings.HasPrefix(identity.TrialName, trialPrefix+"__") {
			return domain.ExecutionResult{}, ErrResultIdentity
		}
		if len(sources) > 0 {
			if err := verifyHarborTaskSource(job.Trials[0], sources[0]); err != nil {
				return domain.ExecutionResult{}, err
			}
		}
		return ParseHarborResult(job.Trials[0], filename, exitCode)
	}
	return domain.ExecutionResult{State: "infrastructure_error", Error: "Harbor job finished without a trial result", ExitCode: exitCode, ResultPath: filename, ImageDigests: []string{}}, nil
}

func ReadHarborResult(ctx context.Context, jobDir, task string, exitCode int) (domain.ExecutionResult, error) {
	return readHarborResult(ctx, jobDir, task, trialDirectoryPrefix(task), exitCode)
}

func ReadHarborResultForTask(ctx context.Context, jobDir, task string, source domain.BenchmarkTask, exitCode int, nativePrefixes ...string) (domain.ExecutionResult, error) {
	if err := source.Validate(); err != nil {
		return domain.ExecutionResult{}, err
	}
	prefix := trialDirectoryPrefix(task)
	if len(nativePrefixes) == 1 {
		prefix = nativePrefixes[0]
	} else if len(nativePrefixes) > 1 {
		return domain.ExecutionResult{}, ErrResultIdentity
	}
	return readHarborResult(ctx, jobDir, task, prefix, exitCode, source)
}

func readHarborResult(ctx context.Context, jobDir, task, trialPrefix string, exitCode int, sources ...domain.BenchmarkTask) (domain.ExecutionResult, error) {
	if !domain.ValidTaskName(task) || !simpleName.MatchString(trialPrefix) {
		return domain.ExecutionResult{}, ErrResultIdentity
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		result, err := readHarborResultOnce(jobDir, task, trialPrefix, exitCode, sources...)
		if err == nil || errors.Is(err, ErrResultIdentity) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return domain.ExecutionResult{State: "infrastructure_error", Error: ErrIncompleteResult.Error(), ExitCode: exitCode, ResultPath: filepath.Join(jobDir, "result.json"), ImageDigests: []string{}}, ErrIncompleteResult
		case <-timer.C:
		}
	}
}

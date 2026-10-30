package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/gateway"
)

func validateAgentKwargs(values map[string]json.RawMessage) error {
	reserved := map[string]bool{"api_key": true, "api_base": true, "api_base_url": true, "base_url": true, "api_format": true, "model": true, "model_name": true, "version": true, "env": true, "environment": true, "config": true, "config_path": true}
	for key, value := range values {
		lower := strings.ToLower(key)
		if reserved[lower] || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") {
			return fmt.Errorf("agent kwargs cannot override native model, endpoint, credentials, environment or container version")
		}
		var scalar any
		if json.Unmarshal(value, &scalar) != nil {
			return fmt.Errorf("invalid agent option")
		}
		switch scalar.(type) {
		case string, float64, bool, nil:
		default:
			return fmt.Errorf("first-release agent kwargs must be scalar values declared by the selected adapter")
		}
	}
	return nil
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	input, ok := decode[domain.RunSpec](w, r)
	if !ok {
		return
	}
	if err := input.Validate(); err != nil {
		problem(w, err)
		return
	}
	if err := validateAgentKwargs(input.AgentKwargs); err != nil {
		problem(w, err)
		return
	}
	env, err := s.store.Environment(r.Context(), input.EnvironmentID)
	if err != nil {
		problem(w, err)
		return
	}
	model, err := s.store.Model(r.Context(), input.ModelID)
	if err != nil {
		problem(w, err)
		return
	}
	provider, err := s.store.Provider(r.Context(), model.ProviderID)
	if err != nil {
		problem(w, err)
		return
	}
	if !provider.HasKey {
		problem(w, fmt.Errorf("the selected provider has no API key"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	inspection, err := s.executor.Inspect(ctx, env)
	if err != nil {
		problem(w, err)
		return
	}
	if !inspection.Ready {
		fail(w, 409, "execution environment is not ready: "+strings.Join(inspection.Issues, "; "))
		return
	}
	versions, available := map[string]string{}, map[string]bool{}
	for _, tool := range inspection.Tools {
		versions[tool.Name], available[tool.Name] = tool.Version, tool.Available
	}
	if !available["uv"] || !available["harbor"] {
		fail(w, 409, "uv and Harbor must be installed and their native --version probes must succeed; tools are never installed or upgraded automatically")
		return
	}
	var agent *domain.Agent
	for i := range inspection.Agents {
		if inspection.Agents[i].Name == input.Agent {
			candidate := inspection.Agents[i]
			agent = &candidate
			break
		}
	}
	if agent == nil || !agent.HarborSupported {
		fail(w, 409, "selected agent has no supported native Harbor adapter in this environment")
		return
	}
	compatible := false
	for _, p := range agent.SupportedProtocols {
		if p == model.Protocol {
			compatible = true
		}
	}
	if !compatible {
		fail(w, 409, "selected agent cannot inject this native protocol through the gateway; no agent or protocol was substituted")
		return
	}
	if agent.Command == "" {
		fail(w, 409, "selected adapter has no supported independent task-container CLI version observation command")
		return
	}
	resolved, err := s.executor.Resolve(ctx, env, domain.SWEBenchVerified())
	if err != nil {
		problem(w, err)
		return
	}
	if err := resolved.Validate(); err != nil {
		problem(w, err)
		return
	}
	hash, err := resolved.Hash()
	if err != nil || resolved.ResolutionHash != hash {
		problem(w, fmt.Errorf("resolved benchmark source hash is inconsistent"))
		return
	}
	var selected *domain.BenchmarkTask
	for _, task := range resolved.Tasks {
		if task.Name == input.TaskNames[0] {
			value := task
			selected = &value
			break
		}
	}
	if selected == nil {
		problem(w, fmt.Errorf("selected task is not present in Harbor's currently resolved benchmark; no substitute task was selected"))
		return
	}
	input.ConfirmPaid = false
	snapshot := domain.RunSnapshot{Spec: input, Environment: env, Model: model, Provider: provider, Suite: resolved.Suite, HarborVersion: versions["harbor"], UVVersion: versions["uv"], Agent: *agent, FormatVersion: domain.SnapshotFormatVersion, RegistryKind: resolved.RegistryKind, RegistryMetadataURL: resolved.RegistryMetadataURL, DatasetDigest: resolved.DatasetDigest, DatasetRevision: resolved.DatasetRevision, ResolvedAt: resolved.ResolvedAt, ResolutionHash: resolved.ResolutionHash, ResolvedTasks: []domain.BenchmarkTask{*selected}, PreflightTools: append([]domain.ToolStatus{}, inspection.Tools...)}
	snapshot.ConfigHash, err = snapshot.ApprovalHash()
	if err != nil {
		problem(w, err)
		return
	}
	if err := snapshot.ValidateSources(); err != nil {
		problem(w, err)
		return
	}
	run, err := s.store.CreateRun(r.Context(), snapshot)
	if err != nil {
		problem(w, err)
		return
	}
	if err = s.store.AddEvent(r.Context(), domain.JobEvent{RunID: run.ID, Kind: "draft", Message: "Harbor resolved the current dataset; the exact selected task source is recorded for this draft. No inference authorized or started."}); err != nil {
		problem(w, err)
		return
	}
	write(w, publicRun(run))
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	input, ok := decode[struct {
		ConfirmPaid bool `json:"confirm_paid"`
	}](w, r)
	if !ok {
		return
	}
	if !input.ConfirmPaid {
		fail(w, 400, "explicit approval of provider inference and the approved request/output/time limits is required")
		return
	}
	run, err := s.store.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		problem(w, err)
		return
	}
	if run.State != "draft" {
		problem(w, domain.ErrConflict)
		return
	}
	if err := run.Snapshot.ValidateSources(); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	if len(run.Tasks) != 1 || len(run.Tasks[0].Attempts) != 1 || run.Tasks[0].Name != run.Snapshot.Spec.TaskNames[0] {
		fail(w, http.StatusConflict, "draft task and attempt identities are inconsistent")
		return
	}
	if _, err = s.store.Credential(r.Context(), run.Snapshot.Provider.ID, run.Snapshot.Provider.KeyRevision); err != nil {
		problem(w, err)
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		fail(w, 503, "backend is shutting down")
		return
	}
	if len(s.active) > 0 {
		s.mu.Unlock()
		fail(w, 409, "one run is already active; the first release limits active execution to one")
		return
	}
	if err = s.store.TransitionRun(r.Context(), run.ID, "draft", "preparing", ""); err != nil {
		s.mu.Unlock()
		problem(w, err)
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	active := &activeRun{cancel: cancel}
	s.active[run.ID] = active
	s.wg.Add(1)
	s.mu.Unlock()
	if err = s.store.AddEvent(context.Background(), domain.JobEvent{RunID: run.ID, Kind: "authorized", Message: "Explicit start approval received; one task, one attempt, no Harbor trial retries."}); err != nil {
		cancel()
		s.mu.Lock()
		delete(s.active, run.ID)
		s.mu.Unlock()
		s.store.TransitionRun(context.Background(), run.ID, "preparing", "failed", "Unable to persist run authorization")
		s.wg.Done()
		problem(w, err)
		return
	}
	go s.execute(ctx, run, active)
	updated, err := s.store.Run(r.Context(), run.ID)
	if err != nil {
		problem(w, err)
		return
	}
	write(w, publicRun(updated))
}

func (s *Server) execute(ctx context.Context, run domain.Run, active *activeRun) {
	defer s.wg.Done()
	defer func() { active.cancel(); s.mu.Lock(); delete(s.active, run.ID); s.mu.Unlock() }()
	attempt := run.Tasks[0].Attempts[0]
	if ctx.Err() != nil {
		state := "cancelled"
		if s.ctx.Err() != nil {
			state = "interrupted"
		}
		attempt.State = state
		ended := domain.Now()
		attempt.FinishedAt = &ended
		s.store.SaveAttempt(context.Background(), attempt)
		s.finishRun(run.ID, state, "")
		return
	}
	now := domain.Now()
	attempt.StartedAt = &now
	attempt.State = "preparing"
	if err := s.store.SaveAttempt(context.Background(), attempt); err != nil {
		s.finishRun(run.ID, "failed", "Unable to persist attempt admission")
		return
	}
	key, err := s.store.Credential(ctx, run.Snapshot.Provider.ID, run.Snapshot.Provider.KeyRevision)
	if err != nil {
		if ctx.Err() != nil {
			state := "cancelled"
			if s.ctx.Err() != nil {
				state = "interrupted"
			}
			attempt.State = state
			ended := domain.Now()
			attempt.FinishedAt = &ended
			s.store.SaveAttempt(context.Background(), attempt)
			s.finishRun(run.ID, state, "")
			return
		}
		s.failAttempt(attempt, "credential_error", "Provider credential reference is unavailable")
		s.finishRun(run.ID, "failed", "Provider credential reference is unavailable")
		return
	}
	lease := gateway.Lease{RunID: run.ID, TaskID: attempt.TaskID, AttemptID: attempt.ID, Provider: run.Snapshot.Provider, Model: run.Snapshot.Model, APIKey: key, MaxRequests: run.Snapshot.Spec.MaxRequests, MaxOutputTokens: run.Snapshot.Spec.MaxOutputTokens, Timeout: time.Duration(run.Snapshot.Spec.TimeoutSec) * time.Second, AllowedModelNames: []string{run.Snapshot.Model.Name, "router-forge/" + run.Snapshot.Model.Name, "forge/" + run.Snapshot.Model.Name, "rf/" + run.Snapshot.Model.Name}, RequireAgentObservation: true}
	token, err := s.gateway.Register(lease)
	if err != nil {
		s.failAttempt(attempt, "gateway_error", "Gateway lease could not be registered")
		s.finishRun(run.ID, "failed", "Gateway lease registration failed")
		return
	}
	defer s.gateway.Revoke(token)
	s.mu.Lock()
	active.token = token
	s.mu.Unlock()
	redact := func(message string) string {
		message = strings.ReplaceAll(message, key, "[redacted]")
		message = strings.ReplaceAll(message, token, "[redacted]")
		if len(message) > 4096 {
			message = message[:4096]
		}
		return message
	}
	snapshot := run.Snapshot
	snapshot.Spec.ConfirmPaid = true
	spec := domain.ExecutionSpec{RunID: run.ID, TaskID: attempt.TaskID, AttemptID: attempt.ID, JobName: attempt.JobName, Snapshot: snapshot, GatewayToken: token}
	attempt.State = "running"
	if err = s.store.SaveAttempt(context.Background(), attempt); err != nil {
		s.finishRun(run.ID, "failed", "Unable to persist attempt start")
		return
	}
	if err = s.store.TransitionRun(context.Background(), run.ID, "preparing", "running", ""); err != nil {
		if ctx.Err() != nil {
			s.failAttempt(attempt, "cancelled", "Run cancelled before worker start")
			s.finishRun(run.ID, "cancelled", "")
		} else {
			s.failAttempt(attempt, "persistence_error", "Unable to persist worker start")
			s.finishRun(run.ID, "failed", "Unable to persist worker start")
		}
		return
	}
	result, err := s.executor.Execute(ctx, snapshot.Environment, spec, func(event domain.WorkerEvent) {
		s.store.AddEvent(context.Background(), domain.JobEvent{RunID: run.ID, AttemptID: attempt.ID, Timestamp: event.Timestamp, Kind: event.Kind, Message: redact(event.Message)})
	}, func(observation domain.ExecutionObservation) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.mu.Lock()
		owned := s.active[run.ID] == active && !s.closed
		s.mu.Unlock()
		if !owned {
			return domain.ErrConflict
		}
		b, marshalErr := json.Marshal(observation)
		if marshalErr != nil {
			return marshalErr
		}
		for _, secret := range []string{key, token} {
			encoded, _ := json.Marshal(secret)
			if secret != "" && bytes.Contains(b, encoded[1:len(encoded)-1]) {
				return fmt.Errorf("execution observation contains a private credential")
			}
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.store.RecordExecutionObservation(persistCtx, run.ID, attempt.TaskID, attempt.ID, observation); err != nil {
			return err
		}
		if observation.Kind == "agent" {
			return s.gateway.AdmitObservedAgent(token, run.ID, attempt.TaskID, attempt.ID)
		}
		return nil
	})
	readCtx, cancelRead := context.WithTimeout(context.Background(), 10*time.Second)
	current, observationErr := s.store.Run(readCtx, run.ID)
	cancelRead()
	if observationErr != nil {
		err = fmt.Errorf("unable to verify persisted execution observations: %w", observationErr)
	} else if current.Snapshot.Runtime != nil && current.Snapshot.Runtime.AgentVersion != "" {
		actual := current.Snapshot.Runtime.AgentVersion
		if result.AgentVersion != "" && result.AgentVersion != actual && err == nil {
			err = fmt.Errorf("worker result disagrees with the independently persisted container agent version")
		}
		result.AgentVersion = actual
	} else if result.State == "completed" && err == nil {
		err = fmt.Errorf("worker completed without a durable independent container agent version observation")
		result.ExceptionType = "MissingAgentVersionObservation"
	}
	state := result.State
	if err != nil {
		if ctx.Err() != nil {
			if s.ctx.Err() != nil {
				state = "interrupted"
			} else {
				state = "cancelled"
			}
		} else {
			state = "failed"
		}
		result.Error = redact(err.Error())
	}
	if state == "" {
		state = "failed"
	}
	if result.ExceptionType != "" && state == "completed" {
		state = "failed"
	}
	if state == "completed" && result.Reward == nil {
		state = "failed"
		result.ExceptionType = "MissingVerifierResult"
		result.Error = "Worker completed without an independent verifier reward"
	}
	if state != "completed" && state != "failed" && state != "cancelled" && state != "interrupted" {
		state = "failed"
	}
	attempt.State = state
	attempt.TrialName = result.TrialName
	attempt.Reward = result.Reward
	attempt.ExceptionType = result.ExceptionType
	attempt.Error = redact(result.Error)
	attempt.ExitCode = &result.ExitCode
	attempt.ResultPath = result.ResultPath
	attempt.ImageDigests = append([]string{}, result.ImageDigests...)
	attempt.AgentVersion = result.AgentVersion
	attempt.HarborMetrics = safeMetrics(result.HarborMetrics)
	finished := domain.Now()
	attempt.FinishedAt = &finished
	if err = s.store.SaveAttempt(context.Background(), attempt); err != nil {
		s.finishRun(run.ID, "failed", "Unable to persist final verifier result")
		return
	}
	s.store.AddEvent(context.Background(), domain.JobEvent{RunID: run.ID, AttemptID: attempt.ID, Kind: state, Message: fmt.Sprintf("Execution %s; CLI exit %d; reward and infrastructure exception are tracked separately.", state, result.ExitCode)})
	s.finishRun(run.ID, state, attempt.Error)
}

func safeMetrics(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := map[string]json.RawMessage{}
	for _, key := range []string{"n_input_tokens", "n_output_tokens", "n_cache_tokens", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd", "cost"} {
		if value, ok := values[key]; ok {
			var number json.Number
			decoder := json.NewDecoder(strings.NewReader(string(value)))
			decoder.UseNumber()
			if decoder.Decode(&number) == nil {
				out[key] = value
			}
		}
	}
	b, _ := json.Marshal(out)
	return b
}
func (s *Server) failAttempt(a domain.Attempt, kind, message string) {
	a.State = "failed"
	a.ExceptionType = kind
	a.Error = message
	now := domain.Now()
	a.FinishedAt = &now
	s.store.SaveAttempt(context.Background(), a)
}
func (s *Server) finishRun(id, state, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := s.store.Run(ctx, id)
	if err != nil {
		return
	}
	if r.State == "preparing" || r.State == "running" || r.State == "cancelling" {
		s.store.TransitionRun(ctx, id, r.State, state, message)
	}
}
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.store.Run(r.Context(), id)
	if err != nil {
		problem(w, err)
		return
	}
	if run.State == "draft" {
		if err = s.store.TransitionRun(r.Context(), id, "draft", "cancelled", ""); err != nil {
			problem(w, err)
			return
		}
	} else {
		if run.State != "preparing" && run.State != "running" && run.State != "cancelling" {
			problem(w, domain.ErrConflict)
			return
		}
		s.mu.Lock()
		active := s.active[id]
		if active == nil {
			s.mu.Unlock()
			problem(w, domain.ErrConflict)
			return
		}
		if run.State == "preparing" || run.State == "running" {
			err = s.store.TransitionRun(r.Context(), id, run.State, "cancelling", "")
		}
		if err == nil {
			active.cancel()
			if active.token != "" {
				s.gateway.Revoke(active.token)
			}
		}
		s.mu.Unlock()
		if err != nil {
			problem(w, err)
			return
		}
	}
	s.store.AddEvent(context.Background(), domain.JobEvent{RunID: id, Kind: "cancel_requested", Message: "Explicit cancellation of this owned run only."})
	updated, err := s.store.Run(r.Context(), id)
	if err != nil {
		problem(w, err)
		return
	}
	write(w, publicRun(updated))
}

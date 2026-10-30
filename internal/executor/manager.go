package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"golang.org/x/net/http2"
)

const maxControlBody = 512 << 10
const maxModelBody = 8 << 20
const maxPendingCommands = 16
const maxModelStreams = 8

var ErrWorkerConnection = errors.New("worker connection lost; work was interrupted and was not restarted")

type workerCommand struct {
	ID               string                    `json:"id"`
	Kind             string                    `json:"kind"`
	Target           string                    `json:"target,omitempty"`
	Environment      domain.Environment        `json:"environment"`
	Suite            *domain.Suite             `json:"suite,omitempty"`
	Spec             *domain.ExecutionSpec     `json:"spec,omitempty"`
	MockImageID      string                    `json:"mock_image_id,omitempty"`
	NetworkRuntime   *domain.ExecutionNetworks `json:"network_runtime,omitempty"`
	NetworkOperation *domain.NetworkOperation  `json:"network_operation,omitempty"`
}
type commandReply struct {
	ID           string                    `json:"id"`
	Error        string                    `json:"error,omitempty"`
	Inspection   *domain.Inspection        `json:"inspection,omitempty"`
	Tasks        []domain.BenchmarkTask    `json:"tasks,omitempty"`
	Resolved     *domain.ResolvedBenchmark `json:"resolved,omitempty"`
	Result       *domain.ExecutionResult   `json:"result,omitempty"`
	MockResult   *mockIntegrationResult    `json:"mock_result,omitempty"`
	NetworkCheck *domain.NetworkCheck      `json:"network_check,omitempty"`
}

type observationReply struct {
	ID          string                      `json:"id"`
	RunID       string                      `json:"run_id"`
	TaskID      string                      `json:"task_id"`
	AttemptID   string                      `json:"attempt_id"`
	Observation domain.ExecutionObservation `json:"observation"`
}
type eventReply struct {
	ID    string             `json:"id"`
	Event domain.WorkerEvent `json:"event"`
}
type callback struct {
	emit  func(domain.WorkerEvent)
	event domain.WorkerEvent
}
type pendingCommand struct {
	reply       chan commandReply
	emit        func(domain.WorkerEvent)
	observe     func(domain.ExecutionObservation) error
	token       string
	redact      func(string) string
	execute     bool
	spec        *domain.ExecutionSpec
	ctx         context.Context
	observation sync.Mutex
	completed   bool
	agentReady  atomic.Bool
}
type sessionSlot struct {
	fingerprint string
	started     chan struct{}
	session     *workerSession
	err         error
}

type Manager struct {
	workerDir string
	gateway   http.Handler
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	sessions  map[string]*sessionSlot
	closed    bool
	events    chan callback
	deployMu  sync.Mutex
	localRoot string
}

func New(workerDir string, gateway http.Handler) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{workerDir: workerDir, gateway: gateway, ctx: ctx, cancel: cancel, sessions: make(map[string]*sessionSlot), events: make(chan callback, 128)}
	go func() {
		for {
			select {
			case c := <-m.events:
				invokeCallback(c)
			case <-ctx.Done():
				return
			}
		}
	}()
	return m
}
func invokeCallback(c callback) { defer func() { _ = recover() }(); c.emit(c.event) }
func environmentFingerprint(e domain.Environment) string {
	b, _ := json.Marshal(e)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (m *Manager) getSession(ctx context.Context, e domain.Environment) (*workerSession, error) {
	e = clearLegacyNetwork(publicEnvironment(e))
	if err := validateEnvironment(e); err != nil {
		return nil, err
	}
	if e.ID == "" {
		return nil, errors.New("environment ID is required")
	}
	fingerprint := environmentFingerprint(e)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("executor manager is closed")
	}
	slot := m.sessions[fingerprint]
	if slot != nil {
		select {
		case <-slot.started:
			if slot.session != nil {
				select {
				case <-slot.session.done:
					delete(m.sessions, fingerprint)
					slot = nil
				default:
				}
			} else if slot.err != nil {
				delete(m.sessions, fingerprint)
				slot = nil
			}
		default:
		}
	}
	opener := slot == nil
	if opener && len(m.sessions) >= 32 {
		m.mu.Unlock()
		return nil, errors.New("worker profile session limit reached; close idle managers before creating more profiles")
	}
	if opener {
		slot = &sessionSlot{fingerprint: fingerprint, started: make(chan struct{})}
		m.sessions[fingerprint] = slot
	}
	m.mu.Unlock()
	if opener {
		session, err := m.startSession(e)
		m.mu.Lock()
		slot.session = session
		slot.err = err
		close(slot.started)
		m.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, errors.New("executor manager is closed")
	case <-slot.started:
	}
	if slot.err != nil {
		return nil, slot.err
	}
	return slot.session, nil
}

type workerSession struct {
	manager       *Manager
	process       *ownedProcess
	conn          *stdioConn
	commands      chan workerCommand
	cancellations chan workerCommand
	ready         chan struct{}
	readyOnce     sync.Once
	done          chan struct{}
	doneOnce      sync.Once
	mu            sync.Mutex
	pending       map[string]*pendingCommand
	modelStreams  chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

func (m *Manager) startSession(e domain.Environment) (*workerSession, error) {
	startup, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	target, path, err := m.prepareWorker(startup, e)
	if err != nil {
		return nil, err
	}
	command, err := BuildLaunchCommand(target, path)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = transportEnvironment()
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	sessionCtx, sessionCancel := context.WithCancel(m.ctx)
	proc, err := startOwned(context.Background(), cmd)
	if err != nil {
		sessionCancel()
		stdin.Close()
		stdout.Close()
		return nil, errors.New("could not start bundled worker using non-interactive environment transport")
	}
	s := &workerSession{manager: m, process: proc, conn: newStdioConn(stdout, stdin), commands: make(chan workerCommand, maxPendingCommands), cancellations: make(chan workerCommand, maxPendingCommands), ready: make(chan struct{}), done: make(chan struct{}), pending: make(map[string]*pendingCommand), modelStreams: make(chan struct{}, maxModelStreams), ctx: sessionCtx, cancel: sessionCancel}
	server := &http2.Server{MaxConcurrentStreams: 32, MaxReadFrameSize: 16384, MaxUploadBufferPerConnection: 1 << 20, MaxUploadBufferPerStream: 64 << 10, WriteByteTimeout: 30 * time.Second}
	go func() {
		server.ServeConn(s.conn, &http2.ServeConnOpts{Context: sessionCtx, Handler: s, BaseConfig: &http.Server{MaxHeaderBytes: 64 << 10}})
		s.disconnect()
	}()
	go func() { _ = proc.wait(); s.disconnect() }()
	select {
	case <-s.ready:
		return s, nil
	case <-s.done:
		return nil, ErrWorkerConnection
	case <-startup.Done():
		s.disconnect()
		return nil, errors.New("worker did not complete its versioned stdio handshake")
	}
}
func (s *workerSession) disconnect() {
	s.doneOnce.Do(func() {
		_ = s.conn.Close()
		s.cancel()
		close(s.done)
		go func() {
			timer := time.NewTimer(15 * time.Second)
			defer timer.Stop()
			select {
			case <-s.process.done:
			case <-timer.C:
				s.process.stop()
			}
		}()
	})
}

func (m *Manager) Inspect(ctx context.Context, e domain.Environment) (domain.Inspection, error) {
	r, err := m.request(ctx, e, workerCommand{Kind: "inspect", Environment: e}, nil)
	if err != nil {
		return domain.Inspection{}, err
	}
	if r.Inspection == nil {
		return domain.Inspection{}, errors.New("worker returned no inspection")
	}
	return *r.Inspection, nil
}
func (m *Manager) Resolve(ctx context.Context, e domain.Environment, suite domain.Suite) (domain.ResolvedBenchmark, error) {
	r, err := m.request(ctx, e, workerCommand{Kind: "resolve", Environment: e, Suite: &suite}, nil)
	if err != nil {
		return domain.ResolvedBenchmark{}, err
	}
	if r.Resolved == nil {
		return domain.ResolvedBenchmark{}, errors.New("worker returned no resolved benchmark")
	}
	if err := r.Resolved.Validate(); err != nil {
		return domain.ResolvedBenchmark{}, err
	}
	hash, err := r.Resolved.Hash()
	if err != nil || r.Resolved.ResolutionHash != hash {
		return domain.ResolvedBenchmark{}, errors.New("worker returned inconsistent benchmark provenance")
	}
	return *r.Resolved, nil
}
func (m *Manager) Tasks(ctx context.Context, e domain.Environment, suite domain.Suite) ([]domain.BenchmarkTask, error) {
	resolved, err := m.Resolve(ctx, e, suite)
	return resolved.Tasks, err
}
func (m *Manager) CheckNetwork(ctx context.Context, e domain.Environment, operation domain.NetworkOperation) (domain.NetworkCheck, error) {
	if err := operation.Profile.ValidateSaved(); err != nil {
		return domain.NetworkCheck{}, err
	}
	if operation.Profile.Scope != domain.NetworkEnvironment || operation.Profile.EnvironmentID != e.ID || !domain.ValidNetworkURL(operation.TargetURL, false) {
		return domain.NetworkCheck{}, errors.New("network check requires the selected execution host and an explicit safe target URL")
	}
	data, err := json.Marshal(operation)
	if err != nil || len(data) > maxControlBody/2 {
		return domain.NetworkCheck{}, errors.New("private network check exceeded its limit")
	}
	var copied domain.NetworkOperation
	if json.Unmarshal(data, &copied) != nil {
		return domain.NetworkCheck{}, errors.New("invalid private network check")
	}
	r, err := m.request(ctx, e, workerCommand{Kind: "check-network", Environment: e, NetworkOperation: &copied}, nil)
	if err != nil {
		return domain.NetworkCheck{}, err
	}
	if r.NetworkCheck == nil || r.NetworkCheck.ProfileID != operation.Profile.ID || r.NetworkCheck.ProfileRevision != operation.Profile.Revision || r.NetworkCheck.TargetURL != operation.TargetURL {
		return domain.NetworkCheck{}, errors.New("worker returned inconsistent network check evidence")
	}
	return *r.NetworkCheck, nil
}

func (m *Manager) Execute(ctx context.Context, e domain.Environment, spec domain.ExecutionSpec, emit func(domain.WorkerEvent), observe func(domain.ExecutionObservation) error) (domain.ExecutionResult, error) {
	if observe == nil {
		return infrastructureResult("durable execution observation sink is required", ""), errors.New("durable execution observation sink is required")
	}
	r, err := m.request(ctx, e, workerCommand{Kind: "execute", Environment: e, Spec: &spec}, emit, observe)
	if r.Result != nil {
		return *r.Result, err
	}
	state := "infrastructure_error"
	exception := "WorkerConnectionError"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		state = "cancelled"
		exception = ""
	} else if errors.Is(err, ErrWorkerConnection) {
		state = "interrupted"
	}
	message := "worker execution did not produce a result"
	if err != nil {
		message = err.Error()
	}
	return domain.ExecutionResult{State: state, ExitCode: -1, ExceptionType: exception, Error: message}, err
}
func (m *Manager) request(ctx context.Context, e domain.Environment, command workerCommand, emit func(domain.WorkerEvent), observations ...func(domain.ExecutionObservation) error) (commandReply, error) {
	if err := ctx.Err(); err != nil {
		return commandReply{}, err
	}
	if command.Kind != "execute" && command.Kind != "verify-mock" && command.Kind != "check-network" {
		var err error
		command.NetworkRuntime, err = privateCommandNetworks(e)
		if err != nil {
			return commandReply{}, err
		}
	}
	command.Environment = publicEnvironment(command.Environment)
	if command.Spec != nil {
		data, err := json.Marshal(command.Spec)
		if err != nil || len(data) > maxControlBody {
			return commandReply{}, errors.New("private execution command exceeded its limit")
		}
		var copied domain.ExecutionSpec
		if json.Unmarshal(data, &copied) != nil {
			return commandReply{}, errors.New("invalid private execution command")
		}
		command.Spec = &copied
	}
	s, err := m.getSession(ctx, e)
	if err != nil {
		return commandReply{}, err
	}
	command.ID = domain.NewID("cmd")
	p := &pendingCommand{reply: make(chan commandReply, 1), emit: emit, execute: command.Kind == "execute" || command.Kind == "verify-mock", spec: command.Spec, ctx: ctx}
	p.redact = func(value string) string { return redactCommandText(value, command) }
	p.agentReady.Store(command.Kind != "execute")
	if len(observations) > 0 {
		p.observe = observations[0]
	}
	if command.Spec != nil {
		p.token = command.Spec.GatewayToken
	}
	s.mu.Lock()
	if len(s.pending) >= maxPendingCommands {
		s.mu.Unlock()
		return commandReply{}, errors.New("worker command capacity reached")
	}
	if p.execute {
		for _, existing := range s.pending {
			if existing.execute {
				s.mu.Unlock()
				return commandReply{}, errors.New("first release allows one active attempt per worker")
			}
		}
	}
	s.pending[command.ID] = p
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, command.ID); s.mu.Unlock() }()
	select {
	case s.commands <- command:
	case <-ctx.Done():
		return commandReply{}, ctx.Err()
	case <-s.done:
		return commandReply{}, ErrWorkerConnection
	}
	select {
	case r := <-p.reply:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		return r, nil
	case <-s.done:
		return commandReply{}, ErrWorkerConnection
	case <-ctx.Done():
		cancellation := workerCommand{ID: domain.NewID("cancel"), Kind: "cancel", Target: command.ID}
		select {
		case s.cancellations <- cancellation:
		case <-s.done:
			return commandReply{}, ErrWorkerConnection
		default:
			s.disconnect()
			return commandReply{}, ctx.Err()
		}
		timeout := time.NewTimer(10 * time.Second)
		defer timeout.Stop()
		select {
		case r := <-p.reply:
			return r, ctx.Err()
		case <-s.done:
			return commandReply{}, ctx.Err()
		case <-timeout.C:
			s.disconnect()
			return commandReply{}, ctx.Err()
		}
	}
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.cancel()
	var sessions []*workerSession
	for _, slot := range m.sessions {
		if slot.session != nil {
			sessions = append(sessions, slot.session)
		}
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.disconnect()
	}
	timeout := time.NewTimer(16 * time.Second)
	defer timeout.Stop()
	for _, s := range sessions {
		select {
		case <-s.process.done:
		case <-timeout.C:
			return errors.New("owned worker did not exit before shutdown deadline")
		}
	}
	return nil
}

func (s *workerSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Router-Forge-Version", domain.Version)
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		s.gateway(w, r)
		return
	}
	if r.Header.Get("X-Router-Forge-Version") != domain.Version {
		http.Error(w, "worker version mismatch", http.StatusConflict)
		return
	}
	switch r.URL.Path {
	case "/commands":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		s.readyOnce.Do(func() { close(s.ready) })
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		var command workerCommand
		select {
		case command = <-s.cancellations:
		default:
			select {
			case command = <-s.cancellations:
			case command = <-s.commands:
			case <-r.Context().Done():
				return
			case <-s.done:
				return
			case <-timer.C:
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(command)
	case "/results":
		var result commandReply
		if r.Method != http.MethodPost || decodeBody(w, r, &result, maxControlBody) != nil {
			return
		}
		s.mu.Lock()
		p := s.pending[result.ID]
		s.mu.Unlock()
		if p != nil {
			p.observation.Lock()
			p.completed = true
			p.agentReady.Store(false)
			p.observation.Unlock()
			if p.redact != nil {
				result.Error = p.redact(result.Error)
				if result.Result != nil {
					*result.Result = privateExecutionResult(*result.Result, p.redact)
				}
				if result.Inspection != nil {
					*result.Inspection = privateInspection(*result.Inspection, p.redact)
				}
				if result.NetworkCheck != nil {
					for i, issue := range result.NetworkCheck.Issues {
						result.NetworkCheck.Issues[i] = p.redact(issue)
					}
				}
			} else if p.token != "" {
				result.Error = strings.ReplaceAll(result.Error, p.token, "[redacted]")
				if result.Result != nil {
					result.Result.Error = strings.ReplaceAll(result.Result.Error, p.token, "[redacted]")
				}
			}
			select {
			case p.reply <- result:
			default:
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case "/observations":
		var value observationReply
		if r.Method != http.MethodPost || decodeBody(w, r, &value, 128<<10) != nil {
			return
		}
		s.mu.Lock()
		p := s.pending[value.ID]
		s.mu.Unlock()
		if p == nil || !p.execute || p.spec == nil || p.observe == nil || value.RunID != p.spec.RunID || value.TaskID != p.spec.TaskID || value.AttemptID != p.spec.AttemptID {
			http.Error(w, "observation does not belong to an active execution", http.StatusConflict)
			return
		}
		p.observation.Lock()
		defer p.observation.Unlock()
		if p.completed || p.ctx == nil || p.ctx.Err() != nil {
			http.Error(w, "execution is no longer accepting observations", http.StatusConflict)
			return
		}
		if err := value.Observation.Validate(); err != nil {
			http.Error(w, "invalid bounded execution observation", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(value.Observation)
		if p.token != "" && bytes.Contains(b, []byte(p.token)) {
			http.Error(w, "execution observation contains a private token", http.StatusBadRequest)
			return
		}
		value.Observation = privateObservation(p.spec, value.Observation)
		next, err := domain.CloneSnapshot(p.spec.Snapshot)
		if err != nil || next.ApplyObservation(value.Observation) != nil {
			http.Error(w, "observation cannot alter or replace the active execution snapshot", http.StatusBadRequest)
			return
		}
		if err := p.observe(value.Observation); err != nil {
			http.Error(w, "execution observation could not be persisted; inference must remain blocked", http.StatusInternalServerError)
			return
		}
		p.spec.Snapshot = next
		if value.Observation.Kind == "agent" {
			p.agentReady.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	case "/events":
		var event eventReply
		if r.Method != http.MethodPost || decodeBody(w, r, &event, 8192) != nil {
			return
		}
		s.mu.Lock()
		p := s.pending[event.ID]
		s.mu.Unlock()
		if p != nil && p.emit != nil {
			if !simpleName.MatchString(event.Event.Kind) {
				http.Error(w, "invalid event kind", 400)
				return
			}
			if p.redact != nil {
				event.Event.Message = p.redact(event.Event.Message)
			}
			event.Event.Message = redactedMessage(event.Event.Message, p.token)
			if event.Event.Timestamp == "" {
				event.Event.Timestamp = domain.Now()
			}
			select {
			case s.manager.events <- callback{emit: p.emit, event: event.Event}:
			default:
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}
func decodeBody(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	if r.ContentLength > limit {
		http.Error(w, "request body too large", 413)
		return errors.New("body too large")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		status := 400
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = 413
		}
		http.Error(w, "invalid bounded JSON request", status)
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "multiple JSON values", 400)
		return errors.New("multiple JSON values")
	}
	return nil
}
func redactedMessage(message, token string) string {
	if token != "" {
		message = strings.ReplaceAll(message, token, "[redacted]")
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == 0 {
			return ' '
		}
		return r
	}, message)
}
func modelRouteAllowed(method, p string) bool {
	if method == http.MethodPost && (p == "/v1/chat/completions" || p == "/v1/responses" || p == "/v1/messages") {
		return true
	}
	if !strings.HasPrefix(p, "/v1/responses/") {
		return false
	}
	suffix := strings.TrimPrefix(p, "/v1/responses/")
	if method == http.MethodGet {
		return simpleName.MatchString(suffix)
	}
	if method == http.MethodPost && strings.HasSuffix(suffix, "/cancel") {
		return simpleName.MatchString(strings.TrimSuffix(suffix, "/cancel"))
	}
	return false
}
func (s *workerSession) gateway(w http.ResponseWriter, r *http.Request) {
	if s.manager.gateway == nil || !modelRouteAllowed(r.Method, r.URL.Path) || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	token, valid := leaseHeader(r)
	if !valid {
		http.Error(w, "invalid active attempt lease", 401)
		return
	}
	authorized, ready := false, false
	s.mu.Lock()
	for _, p := range s.pending {
		if p.execute && p.token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(p.token)) == 1 {
			authorized, ready = true, p.agentReady.Load() && p.ctx != nil && p.ctx.Err() == nil
			break
		}
	}
	s.mu.Unlock()
	if !authorized {
		http.Error(w, "invalid active attempt lease", 401)
		return
	}
	if !ready {
		http.Error(w, "independent agent version has not been durably acknowledged", http.StatusConflict)
		return
	}
	select {
	case s.modelStreams <- struct{}{}:
		defer func() { <-s.modelStreams }()
	default:
		http.Error(w, "relay concurrency limit reached", 429)
		return
	}
	if r.ContentLength > maxModelBody {
		http.Error(w, "request body too large", 413)
		return
	}
	if r.Body != nil {
		data, err := io.ReadAll(io.LimitReader(r.Body, maxModelBody+1))
		_ = r.Body.Close()
		if err != nil || len(data) > maxModelBody {
			http.Error(w, "request body too large", 413)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		r.ContentLength = int64(len(data))
	}
	s.manager.gateway.ServeHTTP(w, r)
}
func (r commandReply) String() string { return fmt.Sprintf("command %s", r.ID) }

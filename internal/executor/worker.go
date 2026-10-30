package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
	"golang.org/x/net/http2"
)

type WorkerBackend interface {
	Inspect(context.Context, domain.Environment) (domain.Inspection, error)
	Resolve(context.Context, domain.Environment, domain.Suite) (domain.ResolvedBenchmark, error)
	Tasks(context.Context, domain.Environment, domain.Suite) ([]domain.BenchmarkTask, error)
	Execute(context.Context, domain.Environment, domain.ExecutionSpec, func(domain.WorkerEvent), func(domain.ExecutionObservation) error) (domain.ExecutionResult, error)
}

type workerRoundTripper struct{ conn *http2.ClientConn }

func (t workerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.conn.RoundTrip(r)
}

func RunWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	return ServeWorker(ctx, input, output, nil)
}

// ServeWorker permits embedding a backend; nil selects native Harbor execution.
func ServeWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser, factory func(*http.Client) WorkerBackend) error {
	conn := newStdioConn(input, output)
	defer conn.Close()
	transport := &http2.Transport{AllowHTTP: true, MaxHeaderListSize: 64 << 10, MaxReadFrameSize: 16384, ReadIdleTimeout: 15 * time.Second, PingTimeout: 15 * time.Second, WriteByteTimeout: 30 * time.Second}
	clientConn, err := transport.NewClientConn(conn)
	if err != nil {
		return ErrWorkerConnection
	}
	defer clientConn.Close()
	client := &http.Client{Transport: workerRoundTripper{conn: clientConn}}
	root, cancel := context.WithCancel(ctx)
	defer cancel()
	backend := WorkerBackend(newNativeWorker(client))
	if factory != nil {
		backend = factory(client)
	}
	if backend == nil {
		return errors.New("worker backend is unavailable")
	}
	var mu sync.Mutex
	active := map[string]context.CancelFunc{}
	executions := map[string]bool{}
	cancelled := map[string]bool{}
	events := make(chan eventReply, 128)
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	var jobs sync.WaitGroup
	eventDone := make(chan struct{})
	go func() {
		defer close(eventDone)
		for {
			select {
			case event := <-events:
				if err := postWorker(root, client, "/events", event); err != nil {
					fail(err)
					return
				}
			case <-root.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		mu.Lock()
		for _, stop := range active {
			stop()
		}
		mu.Unlock()
		done := make(chan struct{})
		go func() { jobs.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
		<-eventDone
	}()
	for {
		req, _ := http.NewRequestWithContext(root, http.MethodGet, "http://router-forge/commands", nil)
		req.Header.Set("X-Router-Forge-Version", domain.Version)
		response, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrWorkerConnection
		}
		if response.Header.Get("X-Router-Forge-Version") != domain.Version {
			response.Body.Close()
			return errors.New("parent/worker version mismatch")
		}
		if response.StatusCode == http.StatusNoContent {
			response.Body.Close()
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return ErrWorkerConnection
		}
		var command workerCommand
		b, err := io.ReadAll(io.LimitReader(response.Body, maxControlBody+1))
		response.Body.Close()
		if err != nil || len(b) > maxControlBody || json.Unmarshal(b, &command) != nil || !simpleName.MatchString(command.ID) {
			return errors.New("invalid bounded worker command")
		}
		if command.Kind == "cancel" {
			mu.Lock()
			stop := active[command.Target]
			if stop == nil && len(cancelled) < maxPendingCommands {
				cancelled[command.Target] = true
			}
			mu.Unlock()
			if stop != nil {
				stop()
			}
			continue
		}
		mu.Lock()
		alreadyCancelled := cancelled[command.ID]
		delete(cancelled, command.ID)
		tooMany := len(active) >= 8
		if (command.Kind == "execute" || command.Kind == "verify-mock") && len(executions) > 0 {
			tooMany = true
		}
		if tooMany || alreadyCancelled {
			mu.Unlock()
			reply := commandReply{ID: command.ID, Error: "worker command capacity reached"}
			if alreadyCancelled {
				reply.Error = context.Canceled.Error()
				if command.Kind == "execute" {
					reply.Result = &domain.ExecutionResult{State: "cancelled", ExitCode: -1}
				}
			}
			if err := postWorker(root, client, "/results", reply); err != nil {
				return ErrWorkerConnection
			}
			continue
		}
		commandCtx, stop := context.WithCancel(root)
		active[command.ID] = stop
		if command.Kind == "execute" || command.Kind == "verify-mock" {
			executions[command.ID] = true
		}
		mu.Unlock()
		jobs.Add(1)
		go func(command workerCommand, commandCtx context.Context, stop context.CancelFunc) {
			defer jobs.Done()
			defer stop()
			reply := commandReply{ID: command.ID}
			var callErr error
			switch command.Kind {
			case "inspect":
				value, e := backend.Inspect(commandCtx, command.Environment)
				reply.Inspection = &value
				callErr = e
			case "tasks", "resolve":
				if command.Suite == nil {
					callErr = errors.New("suite is required")
				} else {
					value, e := backend.Resolve(commandCtx, command.Environment, *command.Suite)
					callErr = e
					if command.Kind == "tasks" {
						reply.Tasks = value.Tasks
					} else {
						reply.Resolved = &value
					}
				}
			case "execute":
				if command.Spec == nil {
					callErr = errors.New("execution spec is required")
				} else {
					emit := func(event domain.WorkerEvent) {
						event.Message = redactedMessage(event.Message, command.Spec.GatewayToken)
						if event.Timestamp == "" {
							event.Timestamp = domain.Now()
						}
						select {
						case events <- eventReply{ID: command.ID, Event: event}:
						default:
						}
					}
					observe := func(value domain.ExecutionObservation) error {
						if err := value.Validate(); err != nil {
							return err
						}
						return postWorker(commandCtx, client, "/observations", observationReply{ID: command.ID, RunID: command.Spec.RunID, TaskID: command.Spec.TaskID, AttemptID: command.Spec.AttemptID, Observation: value})
					}
					result, e := backend.Execute(commandCtx, command.Environment, *command.Spec, emit, observe)
					reply.Result = &result
					callErr = e
				}
			case "verify-mock":
				mock, ok := backend.(mockIntegrationBackend)
				if !ok || command.Spec == nil {
					callErr = errors.New("offline fixture backend/spec is unavailable")
				} else {
					emit := func(event domain.WorkerEvent) {
						event.Message = redactedMessage(event.Message, command.Spec.GatewayToken)
						if event.Timestamp == "" {
							event.Timestamp = domain.Now()
						}
						select {
						case events <- eventReply{ID: command.ID, Event: event}:
						default:
						}
					}
					value, err := mock.verifyMockIntegration(commandCtx, command.Environment, *command.Spec, command.MockImageID, emit)
					reply.MockResult = &value
					callErr = err
				}
			default:
				callErr = errors.New("unsupported worker command")
			}
			if callErr != nil {
				reply.Error = redactedMessage(callErr.Error(), "")
				if command.Spec != nil {
					reply.Error = redactedMessage(reply.Error, command.Spec.GatewayToken)
				}
			}
			mu.Lock()
			delete(active, command.ID)
			delete(executions, command.ID)
			mu.Unlock()
			if err := postWorker(root, client, "/results", reply); err != nil {
				fail(err)
			}
		}(command, commandCtx, stop)
		select {
		case <-failures:
			return ErrWorkerConnection
		default:
		}
	}
}
func postWorker(ctx context.Context, client *http.Client, p string, value any) error {
	b, err := json.Marshal(value)
	if err != nil || len(b) > maxControlBody {
		return errors.New("worker result exceeded bounded transport size")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://router-forge"+p, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Router-Forge-Version", domain.Version)
	response, err := client.Do(req)
	if err != nil {
		return ErrWorkerConnection
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
	if response.StatusCode != http.StatusNoContent || response.Header.Get("X-Router-Forge-Version") != domain.Version {
		return ErrWorkerConnection
	}
	return nil
}

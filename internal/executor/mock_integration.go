package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

const mockIntegrationModel = "router-forge-fixture"

type mockIntegrationResult struct {
	Fixture           bool   `json:"fixture"`
	BenchmarkExecuted bool   `json:"benchmark_executed"`
	ModelInference    bool   `json:"model_inference"`
	RunID             string `json:"run_id"`
	TaskID            string `json:"task_id"`
	AttemptID         string `json:"attempt_id"`
	Owner             string `json:"owner"`
	WorkerVersion     string `json:"worker_version"`
	ImageID           string `json:"image_id"`
	ContainerID       string `json:"container_id"`
	RelayIP           string `json:"relay_ip"`
	RelayURL          string `json:"relay_url"`
	JSONStatus        int    `json:"json_status"`
	JSONBody          string `json:"json_body_base64"`
	SSEStatus         int    `json:"sse_status"`
	SSEBody           string `json:"sse_body_base64"`
	AbortStatus       int    `json:"abort_status"`
	AbortPrefix       string `json:"abort_prefix_base64"`
	Aborted           bool   `json:"aborted"`
	ManagementStatus  int    `json:"management_status"`
	WrongAuthStatus   int    `json:"wrong_auth_status"`
	CleanupCompleted  bool   `json:"cleanup_completed"`
}

type mockIntegrationBackend interface {
	verifyMockIntegration(context.Context, domain.Environment, domain.ExecutionSpec, string, func(domain.WorkerEvent)) (mockIntegrationResult, error)
}

func validateMockIntegration(e domain.Environment, s domain.ExecutionSpec, image string) error {
	if s.Snapshot.Spec.ConfirmPaid || s.Snapshot.Model.Name != mockIntegrationModel || s.Snapshot.Model.Protocol != domain.ChatCompletions || s.GatewayToken == "" || !imageIDPattern.MatchString(image) {
		return errors.New("offline verification requires a synthetic fixed model, lease, existing immutable image and no paid confirmation")
	}
	if environmentFingerprint(e) != environmentFingerprint(s.Snapshot.Environment) {
		return errors.New("offline verification environment differs from its frozen fixture")
	}
	for _, id := range []string{s.RunID, s.TaskID, s.AttemptID, s.Snapshot.Model.ID, s.Snapshot.Provider.ID} {
		if !simpleName.MatchString(id) || !strings.HasPrefix(id, "fixture_") {
			return errors.New("offline verification attribution must be explicitly fixture-scoped")
		}
	}
	u, err := url.Parse(s.Snapshot.Provider.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return errors.New("offline verification permits only an injected loopback mock provider")
	}
	return nil
}

func (w *nativeWorker) verifyMockIntegration(ctx context.Context, e domain.Environment, s domain.ExecutionSpec, image string, emit func(domain.WorkerEvent)) (out mockIntegrationResult, runErr error) {
	if err := validateMockIntegration(e, s, image); err != nil {
		return out, err
	}
	if runtime.GOOS != "linux" {
		return out, errors.New("offline Docker relay proof requires a Linux worker")
	}
	bounded, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	docker := probeTool(bounded, e, "docker", []string{"--version"})
	if !docker.Available {
		return out, errors.New("existing Docker executable is unavailable")
	}
	ip, err := inspectDocker(bounded, docker.Path, e.DockerHostGateway)
	if err != nil {
		return out, err
	}
	data, _, err := runCommand(bounded, Command{Path: docker.Path, Args: []string{"image", "inspect", image, "--format", "{{.Id}}"}}, transportEnvironment(), "", nil, 4096)
	if err != nil || strings.TrimSpace(string(data)) != image {
		return out, errors.New("offline verification image is not already present; pulling is forbidden")
	}
	relay, err := startRelay(bounded, w.gateway, ip, s.GatewayToken, domain.ChatCompletions, nil)
	if err != nil {
		return out, err
	}
	defer relay.close()
	owner := domain.NewID("fixture_owner")
	out = mockIntegrationResult{Fixture: true, RunID: s.RunID, TaskID: s.TaskID, AttemptID: s.AttemptID, Owner: owner, WorkerVersion: domain.Version, ImageID: image, RelayIP: ip, RelayURL: relay.URL}
	// This is an isolated transport fixture, never a Harbor trial or task directory.
	tracker := newDockerTracker(docker.Path, transportEnvironment(), owner, "", "", s)
	defer func() {
		if err := tracker.cleanup(); err != nil {
			if runErr == nil {
				runErr = err
			}
			return
		}
		out.CleanupCompleted = true
	}()
	env := append(transportEnvironment(), "ROUTER_FORGE_FIXTURE_TOKEN="+s.GatewayToken)
	args := []string{"run", "--detach", "--rm", "--pull=never", "--network", "bridge", "--label", "io.router-forge.owner=" + owner, "--label", "io.router-forge.run=" + s.RunID, "--label", "io.router-forge.attempt=" + s.AttemptID, "--add-host", "router-forge-gateway:" + ip, "--env", "ROUTER_FORGE_FIXTURE_TOKEN", "--env", "HTTP_PROXY=", "--env", "HTTPS_PROXY=", "--env", "ALL_PROXY=", "--env", "http_proxy=", "--env", "https_proxy=", "--env", "all_proxy=", "--env", "NO_PROXY=router-forge-gateway," + ip, "--entrypoint", "python3", image, "-c", "import time; time.sleep(180)"}
	data, _, err = runCommand(bounded, Command{Path: docker.Path, Args: args}, env, "", nil, 4096)
	if err != nil {
		return out, errors.New("existing offline fixture image could not start its Python entrypoint")
	}
	id := strings.TrimSpace(string(data))
	if !dockerID.MatchString(id) {
		return out, errors.New("offline fixture did not return an immutable container ID")
	}
	container, err := tracker.inspect(bounded, id)
	if err != nil || !OwnsContainer(container, owner, s.RunID, s.AttemptID) || container.Image != image {
		return out, errors.New("offline fixture container ownership/image could not be verified")
	}
	out.ContainerID = id
	lifecycle(emit, "fixture_relay_ready", "Offline Docker fixture relay is ready; no Harbor task or model inference is running")
	endpoint, _ := url.Parse(relay.URL)
	data, _, err = runCommand(bounded, Command{Path: docker.Path, Args: []string{"exec", id, "python3", "-c", mockIntegrationPython, endpoint.Port()}}, transportEnvironment(), "", nil, 64<<10)
	if err != nil {
		return out, errors.New("offline container relay requests failed")
	}
	var received mockIntegrationResult
	if json.Unmarshal(data, &received) != nil {
		return out, errors.New("offline container produced invalid bounded fixture records")
	}
	out.JSONStatus = received.JSONStatus
	out.JSONBody = received.JSONBody
	out.SSEStatus = received.SSEStatus
	out.SSEBody = received.SSEBody
	out.AbortStatus = received.AbortStatus
	out.AbortPrefix = received.AbortPrefix
	out.Aborted = received.Aborted
	out.ManagementStatus = received.ManagementStatus
	out.WrongAuthStatus = received.WrongAuthStatus
	if out.JSONStatus != 200 || out.SSEStatus != 200 || out.AbortStatus != 200 || !out.Aborted || out.ManagementStatus != 404 || out.WrongAuthStatus != 401 {
		return out, errors.New("offline fixture routing, native response, authentication or cancellation check failed")
	}
	lifecycle(emit, "fixture_transport_complete", "Offline fixture native JSON/SSE and abort requests completed; results are not benchmark scores")
	return out, nil
}

const mockIntegrationPython = `import base64, http.client, json, os, sys
port=int(sys.argv[1])
token=os.environ['ROUTER_FORGE_FIXTURE_TOKEN']
record={}
def request(case,stream=False):
    options=',"stream_options":{"include_usage":true}' if stream else ''
    body=(' \n{"model":"router-forge-fixture","messages":[{"role":"user","content":"FIXTURE '+case+' λ — not a benchmark prompt"}],"max_tokens":32,"stream":'+str(stream).lower()+options+'}\n').encode('utf-8')
    conn=http.client.HTTPConnection('router-forge-gateway',port,timeout=30)
    conn.request('POST','/v1/chat/completions',body,{'Authorization':'Bearer '+token,'Content-Type':'application/json'})
    return conn,conn.getresponse()
c,r=request('json')
record['json_status']=r.status
record['json_body_base64']=base64.b64encode(r.read()).decode('ascii')
r.close();c.close()
c,r=request('sse',True)
record['sse_status']=r.status
record['sse_body_base64']=base64.b64encode(r.read()).decode('ascii')
r.close();c.close()
c,r=request('abort',True)
record['abort_status']=r.status
prefix=r.readline()+r.readline()
record['abort_prefix_base64']=base64.b64encode(prefix).decode('ascii')
r.close();c.close()
record['aborted']=True
c=http.client.HTTPConnection('router-forge-gateway',port,timeout=10)
c.request('GET','/commands',headers={'Authorization':'Bearer '+token})
r=c.getresponse();record['management_status']=r.status;r.read();r.close();c.close()
c=http.client.HTTPConnection('router-forge-gateway',port,timeout=10)
c.request('POST','/v1/chat/completions',b'{}',{'Authorization':'Bearer fixture-not-an-authorized-lease'})
r=c.getresponse();record['wrong_auth_status']=r.status;r.read();r.close();c.close()
print(json.dumps(record))
`

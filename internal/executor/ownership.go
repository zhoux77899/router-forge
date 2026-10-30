package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

var dockerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type DockerContainer struct {
	ID      string            `json:"id"`
	Image   string            `json:"image"`
	Labels  map[string]string `json:"labels"`
	Running bool              `json:"running"`
}

func OwnsContainer(container DockerContainer, owner, runID, attemptID string) bool {
	return dockerID.MatchString(container.ID) && owner != "" && container.Labels["io.router-forge.owner"] == owner && container.Labels["io.router-forge.run"] == runID && container.Labels["io.router-forge.attempt"] == attemptID
}

type commandRunner func(context.Context, Command, []string, string, io.Reader, int) ([]byte, int, error)
type dockerTracker struct {
	docker          string
	env             []string
	owner           string
	spec            domain.ExecutionSpec
	attemptDir      string
	jobDir          string
	runner          commandRunner
	observer        func(domain.ExecutionObservation) error
	taskName        string
	trialPrefix     string
	expectedImageID string
	mu              sync.Mutex
	containers      map[string]DockerContainer
	projects        map[string]bool
	digests         map[string]bool
	version         string
	err             error
	ready           chan struct{}
	readyOnce       sync.Once
	probeRequested  chan struct{}
	probeOnce       sync.Once
}

func newDockerTracker(docker string, env []string, owner, attemptDir, jobDir string, spec domain.ExecutionSpec) *dockerTracker {
	name := ""
	if spec.Snapshot.Runtime != nil {
		name = spec.Snapshot.Runtime.TaskName
	} else if len(spec.Snapshot.Spec.TaskNames) == 1 {
		name = spec.Snapshot.Spec.TaskNames[0]
	}
	prefix := trialDirectoryPrefix(name)
	if spec.Snapshot.Runtime != nil {
		prefix = spec.Snapshot.Runtime.TrialPrefix
	}
	return &dockerTracker{docker: docker, env: env, owner: owner, spec: spec, taskName: name, trialPrefix: prefix, attemptDir: attemptDir, jobDir: jobDir, runner: runCommand, containers: map[string]DockerContainer{}, projects: map[string]bool{}, digests: map[string]bool{}, ready: make(chan struct{}), probeRequested: make(chan struct{})}
}
func (t *dockerTracker) requestProbe() {
	t.probeOnce.Do(func() { close(t.probeRequested) })
}
func (t *dockerTracker) command(ctx context.Context, args ...string) ([]byte, error) {
	data, _, err := t.runner(ctx, Command{Path: t.docker, Args: args}, t.env, "", nil, 256<<10)
	return data, err
}
func (t *dockerTracker) inspect(ctx context.Context, id string) (DockerContainer, error) {
	if !dockerID.MatchString(id) {
		return DockerContainer{}, errors.New("invalid container ID")
	}
	data, err := t.command(ctx, "inspect", id, "--format", `{"id":{{json .Id}},"image":{{json .Image}},"labels":{{json .Config.Labels}},"running":{{json .State.Running}}}`)
	var container DockerContainer
	if err != nil || json.Unmarshal(data, &container) != nil || container.ID != id {
		return container, errors.New("owned container metadata is unavailable")
	}
	return container, nil
}
func (t *dockerTracker) list(ctx context.Context, filter string) ([]string, error) {
	data, err := t.command(ctx, "ps", "--all", "--quiet", "--no-trunc", "--filter", filter)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(data))
	if len(ids) > 128 {
		return nil, errors.New("owned Docker resource inventory exceeded its limit")
	}
	for _, id := range ids {
		if !dockerID.MatchString(id) {
			return nil, errors.New("Docker returned an invalid immutable container ID")
		}
	}
	return ids, nil
}
func pathWithin(parent, child string) bool {
	if child == "" {
		return false
	}
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func (t *dockerTracker) projectOwned(container DockerContainer) bool {
	if _, labelled := container.Labels["io.router-forge.owner"]; labelled && !OwnsContainer(container, t.owner, t.spec.RunID, t.spec.AttemptID) {
		return false
	}
	project := container.Labels["com.docker.compose.project"]
	t.mu.Lock()
	known := t.projects[project]
	t.mu.Unlock()
	return known && simpleName.MatchString(project) && pathWithin(t.attemptDir, container.Labels["com.docker.compose.project.working_dir"])
}
func (t *dockerTracker) discoverNativeProjects(ctx context.Context) {
	if t.jobDir == "" || len(t.spec.Snapshot.Spec.TaskNames) != 1 {
		return
	}
	entries, err := os.ReadDir(t.jobDir)
	if err != nil || len(entries) > 1024 {
		return
	}
	families := []string{}
	for _, entry := range entries {
		if entry.IsDir() && simpleName.MatchString(entry.Name()) && strings.HasPrefix(entry.Name(), t.trialPrefix+"__") {
			name := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
					return r
				}
				return '-'
			}, strings.ToLower(entry.Name()))
			t.mu.Lock()
			t.projects[name+"__env"] = true
			t.mu.Unlock()
			families = append(families, name+"__verifier__")
		}
	}
	if len(families) == 0 {
		return
	}
	data, err := t.command(ctx, "ps", "--all", "--format", `{{.Label "com.docker.compose.project"}}`, "--filter", "label=com.docker.compose.project")
	if err != nil {
		return
	}
	for _, project := range strings.Fields(string(data)) {
		if !simpleName.MatchString(project) {
			continue
		}
		for _, family := range families {
			if strings.HasPrefix(project, family) {
				t.mu.Lock()
				t.projects[project] = true
				t.mu.Unlock()
			}
		}
	}
}
func (t *dockerTracker) collect(ctx context.Context) error {
	t.discoverNativeProjects(ctx)
	ids, err := t.list(ctx, "label=io.router-forge.owner="+t.owner)
	if err != nil {
		return err
	}
	for _, id := range ids {
		container, err := t.inspect(ctx, id)
		if err != nil {
			continue
		}
		if !OwnsContainer(container, t.owner, t.spec.RunID, t.spec.AttemptID) {
			continue
		}
		t.mu.Lock()
		t.containers[id] = container
		if project := container.Labels["com.docker.compose.project"]; simpleName.MatchString(project) && pathWithin(t.attemptDir, container.Labels["com.docker.compose.project.working_dir"]) {
			t.projects[project] = true
		}
		t.mu.Unlock()
	}
	t.mu.Lock()
	projects := make([]string, 0, len(t.projects))
	for project := range t.projects {
		projects = append(projects, project)
	}
	t.mu.Unlock()
	for _, project := range projects {
		ids, err := t.list(ctx, "label=com.docker.compose.project="+project)
		if err != nil {
			continue
		}
		for _, id := range ids {
			container, err := t.inspect(ctx, id)
			if err == nil && t.projectOwned(container) {
				t.mu.Lock()
				t.containers[id] = container
				t.mu.Unlock()
			}
		}
	}
	t.mu.Lock()
	count := len(t.containers)
	t.mu.Unlock()
	if count > 32 {
		return errors.New("attempt Docker resource limit exceeded")
	}
	return nil
}
func (t *dockerTracker) observe(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTicker(300 * time.Millisecond)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		probe, stop := context.WithTimeout(ctx, 3*time.Second)
		err := t.collect(probe)
		stop()
		if err == nil {
			t.mu.Lock()
			containers := make([]DockerContainer, 0, len(t.containers))
			for _, container := range t.containers {
				containers = append(containers, container)
			}
			version := t.version
			t.mu.Unlock()
			for _, container := range containers {
				if !container.Running || !OwnsContainer(container, t.owner, t.spec.RunID, t.spec.AttemptID) {
					continue
				}
				if !imageIDPattern.MatchString(container.Image) {
					t.fail(errors.New("native task image identity could not be observed"), cancel)
					return
				}
				if t.expectedImageID != "" && container.Image != t.expectedImageID {
					t.fail(errors.New("native task image differs from the frozen original task image"), cancel)
					return
				}
				t.mu.Lock()
				t.digests[container.Image] = true
				t.mu.Unlock()
				if version == "" {
					select {
					case <-t.probeRequested:
					default:
						continue
					}
					probe, stop := context.WithTimeout(ctx, 3*time.Second)
					script := `PATH=/root/.local/bin:/root/.opencode/bin:/usr/local/bin:/usr/bin:/bin:$PATH; export PATH; exec ` + shellQuote(t.spec.Snapshot.Agent.Command) + ` --version`
					data, err := t.command(probe, "exec", container.ID, "sh", "-c", script)
					stop()
					if err == nil {
						observed, output := observedAgentVersion(data)
						if observed != "" {
							observation := domain.ExecutionObservation{Kind: "agent", Timestamp: domain.Now(), AgentVersion: observed, AgentVersionOutput: output}
							if t.observer == nil {
								t.fail(errors.New("container agent observation has no durable acknowledgment sink"), cancel)
								return
							}
							if err := t.observer(observation); err != nil {
								t.fail(errors.New("container agent observation could not be durably acknowledged; inference remains blocked"), cancel)
								return
							}
							t.mu.Lock()
							t.version = observed
							t.mu.Unlock()
							t.readyOnce.Do(func() { close(t.ready) })
							version = observed
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (t *dockerTracker) fail(err error, cancel context.CancelFunc) {
	t.mu.Lock()
	t.err = err
	t.mu.Unlock()
	cancel()
}
func (t *dockerTracker) snapshot() (string, []string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	digests := make([]string, 0, len(t.digests))
	for digest := range t.digests {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return t.version, digests, t.err
}
func (t *dockerTracker) addDigests(digests ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, digest := range digests {
		t.digests[digest] = true
	}
}
func (t *dockerTracker) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var cleanupErr error
	if err := t.collect(ctx); err != nil {
		cleanupErr = errors.New("owned Docker resource cleanup could not inspect its inventory")
	}
	t.mu.Lock()
	ids := make([]string, 0, len(t.containers))
	for id := range t.containers {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return errors.New("owned Docker resource cleanup exceeded its deadline")
		}
		container, err := t.inspect(ctx, id)
		if err != nil {
			continue
		}
		if !OwnsContainer(container, t.owner, t.spec.RunID, t.spec.AttemptID) && !t.projectOwned(container) {
			continue
		}
		if _, err := t.command(ctx, "rm", "--force", id); err != nil {
			cleanupErr = errors.New("an owned Docker container could not be removed")
		}
	}
	data, err := t.command(ctx, "network", "ls", "--quiet", "--no-trunc", "--filter", "label=io.router-forge.owner="+t.owner)
	if err != nil {
		return errors.New("owned Docker networks could not be inspected during cleanup")
	}
	networks := strings.Fields(string(data))
	if len(networks) > 32 {
		return errors.New("owned Docker network inventory exceeded its limit")
	}
	for _, id := range networks {
		if !dockerID.MatchString(id) {
			continue
		}
		data, err := t.command(ctx, "network", "inspect", id, "--format", `{"id":{{json .Id}},"labels":{{json .Labels}},"containers":{{json .Containers}}}`)
		var network struct {
			ID         string                     `json:"id"`
			Labels     map[string]string          `json:"labels"`
			Containers map[string]json.RawMessage `json:"containers"`
		}
		if err != nil || json.Unmarshal(data, &network) != nil || network.ID != id {
			continue
		}
		if !OwnsContainer(DockerContainer{ID: id, Labels: network.Labels}, t.owner, t.spec.RunID, t.spec.AttemptID) {
			continue
		}
		if len(network.Containers) > 0 {
			cleanupErr = errors.New("owned Docker network remains in use; unrelated containers were not disturbed")
			continue
		}
		if _, err := t.command(ctx, "network", "rm", id); err != nil {
			cleanupErr = errors.New("an owned Docker network could not be removed")
		}
	}
	return cleanupErr
}

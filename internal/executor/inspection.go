package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

var versionPattern = regexp.MustCompile(`\bv?[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:[-+][A-Za-z0-9.-]+)?\b`)
var agentCommands = map[string]string{
	"opencode": "opencode", "claude-code": "claude", "codex": "codex", "copilot-cli": "copilot", "cursor-cli": "cursor-agent", "gemini-cli": "gemini", "aider": "aider", "goose": "goose", "cline-cli": "cline", "qwen-coder": "qwen", "kimi-cli": "kimi", "kimi-code": "kimi", "openhands": "openhands", "openhands-sdk": "openhands", "mini-swe-agent": "mini", "swe-agent": "sweagent", "vibe": "vibe", "pi": "pi", "hermes": "hermes", "junie": "junie", "rovodev-cli": "rovodev", "code-puppy": "code-puppy", "openclaw": "openclaw", "fabric": "fabric", "grok-build": "grok",
}

func IsNativeExecutablePath(goos, p string) bool {
	if !validText(p) {
		return false
	}
	if goos == "windows" {
		return true
	}
	normalized := strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	if isWindowsAbsolute(p) || strings.HasPrefix(normalized, "/mnt/") && len(normalized) > 7 && normalized[6] == '/' {
		return false
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".exe", ".cmd", ".bat", ".ps1":
		return false
	}
	return strings.HasPrefix(p, "/")
}
func findNative(command, configured string) (string, string) {
	var candidates []string
	if configured != "" {
		if !filepath.IsAbs(configured) || !validText(configured) {
			return "", "configured tool path must be absolute on the execution host"
		}
		candidates = []string{configured}
	} else {
		if found, err := exec.LookPath(command); err == nil {
			candidates = append(candidates, found)
		}
		if home, err := os.UserHomeDir(); err == nil {
			for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "bin")} {
				candidates = append(candidates, filepath.Join(dir, command))
			}
		}
	}
	interop := false
	for _, p := range candidates {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if !IsNativeExecutablePath(runtime.GOOS, p) {
			interop = true
			continue
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
			continue
		}
		return p, ""
	}
	if interop {
		return "", "Windows interoperability shim is not a native executable"
	}
	return "", "native executable not found"
}
func probeTool(ctx context.Context, e domain.Environment, name string, args []string) domain.ToolStatus {
	p, reason := findNative(name, e.Tools[name])
	status := domain.ToolStatus{Name: name, Path: p, Detail: reason}
	if p == "" {
		return status
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, _, err := runCommand(probe, Command{Path: p, Args: args}, transportEnvironment(), "", nil, 32768)
	if err != nil {
		status.Detail = "native version probe failed"
		return status
	}
	status.Available = true
	status.VersionOutput = strings.TrimSpace(ansiEscape.ReplaceAllString(string(b), ""))
	status.Version = strings.TrimPrefix(versionPattern.FindString(status.VersionOutput), "v")
	status.Detail = "native host executable; container readiness is separate"
	return status
}

type authCapabilities struct {
	Agent      string   `json:"agent"`
	Connection bool     `json:"supports_model_connection"`
	BaseURL    bool     `json:"supports_overriding_base_url"`
	Formats    []string `json:"supported_api_formats"`
}
type optionsSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func ParseAgentCapabilities(name string, auth, schema []byte) (domain.Agent, error) {
	var c authCapabilities
	var options optionsSchema
	if !simpleName.MatchString(name) || json.Unmarshal(auth, &c) != nil || c.Agent != name || json.Unmarshal(schema, &options) != nil || options.Type != "object" {
		return domain.Agent{}, errors.New("invalid or mismatched Harbor agent capabilities")
	}
	a := domain.Agent{Name: name, Command: agentCommands[name], HarborSupported: true, Schema: append(json.RawMessage(nil), schema...), ContainerInstall: "Harbor native per-task container installation; host credentials are not copied"}
	a.SupportsVersion = propertyAllowsString(options.Properties["version"])
	if c.Connection && c.BaseURL {
		for _, format := range c.Formats {
			var protocol domain.Protocol
			switch format {
			case "openai_chat_completions":
				protocol = domain.ChatCompletions
			case "openai_responses":
				protocol = domain.Responses
			case "anthropic_messages":
				protocol = domain.AnthropicMessages
			}
			if protocol != "" && !containsProtocol(a.SupportedProtocols, protocol) {
				a.SupportedProtocols = append(a.SupportedProtocols, protocol)
			}
		}
	}
	if len(a.SupportedProtocols) == 0 {
		a.Reason = "adapter does not declare a supported fixed-model endpoint override"
	} else if a.Command == "" {
		a.Reason = "adapter has no supported independent container CLI version observation command"
	}
	return a, nil
}
func propertyAllowsString(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var p struct {
		Type  string            `json:"type"`
		AnyOf []json.RawMessage `json:"anyOf"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	if p.Type == "string" {
		return true
	}
	for _, raw := range p.AnyOf {
		if propertyAllowsString(raw) {
			return true
		}
	}
	return false
}
func containsProtocol(protocols []domain.Protocol, p domain.Protocol) bool {
	for _, v := range protocols {
		if v == p {
			return true
		}
	}
	return false
}
func discoverAgents(ctx context.Context, e domain.Environment, harbor string) ([]domain.Agent, error) {
	data, _, err := runCommand(ctx, Command{Path: harbor, Args: []string{"agent", "list", "--json"}}, transportEnvironment(), "", nil, 128<<10)
	if err != nil {
		return nil, errors.New("Harbor native agent list failed")
	}
	var names []string
	if json.Unmarshal(data, &names) != nil || len(names) > 128 {
		return nil, errors.New("invalid bounded Harbor native agent list")
	}
	agents := make([]domain.Agent, len(names))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				name := names[i]
				a := domain.Agent{Name: name, Command: agentCommands[name], HarborSupported: true, Reason: "native auth/schema inspection failed"}
				if !simpleName.MatchString(name) {
					agents[i] = domain.Agent{Name: "invalid", Reason: "invalid native agent name"}
					continue
				}
				probe, cancel := context.WithTimeout(ctx, 15*time.Second)
				auth, _, aerr := runCommand(probe, Command{Path: harbor, Args: []string{"agent", "auth", name, "--json"}}, transportEnvironment(), "", nil, 128<<10)
				schema, _, serr := runCommand(probe, Command{Path: harbor, Args: []string{"agent", "schema", name, "--json"}}, transportEnvironment(), "", nil, 128<<10)
				cancel()
				if aerr == nil && serr == nil {
					if parsed, err := ParseAgentCapabilities(name, auth, schema); err == nil {
						a = parsed
					}
				}
				if a.Command != "" {
					host := probeTool(ctx, domain.Environment{}, a.Command, []string{"--version"})
					a.Path = host.Path
					a.Installed = host.Available
					a.Version = host.Version
					if !host.Available && a.Reason == "" {
						a.Reason = host.Detail + "; native task-container installation remains available"
					}
				}
				agents[i] = a
			}
		}()
	}
	for i := range names {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}
func selectedWorkDir(e domain.Environment) (string, error) {
	if e.WorkDir != "" {
		if !filepath.IsAbs(e.WorkDir) || !validText(e.WorkDir) || runtime.GOOS != "windows" && strings.Contains(e.WorkDir, `\`) {
			return "", errors.New("work directory must be absolute in execution-host path syntax")
		}
		return filepath.Clean(e.WorkDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			root = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(root, "router-forge", "work"), nil
	}
	return filepath.Join(home, ".local", "share", "router-forge", "work"), nil
}
func validateBindIP(value string) error {
	ip := net.ParseIP(value)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || !ip.IsPrivate() || ip.To4() == nil {
		return errors.New("Docker relay requires a private non-loopback IPv4 address")
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return errors.New("could not inspect local network interfaces")
	}
	for _, address := range addrs {
		var local net.IP
		switch v := address.(type) {
		case *net.IPNet:
			local = v.IP
		case *net.IPAddr:
			local = v.IP
		}
		if local.Equal(ip) {
			return nil
		}
	}
	return errors.New("Docker gateway address is not owned by this worker host; configure a reachable local bridge/interface address")
}
func inspectDocker(ctx context.Context, docker string, requested string) (string, error) {
	data, _, err := runCommand(ctx, Command{Path: docker, Args: []string{"info", "--format", "{{json .}}"}}, transportEnvironment(), "", nil, 128<<10)
	var info struct {
		OSType string `json:"OSType"`
	}
	if err != nil || json.Unmarshal(data, &info) != nil {
		return "", errors.New("Docker daemon is not accessible using existing user permissions")
	}
	if info.OSType != "linux" {
		return "", errors.New("SWE-bench requires native Linux task containers")
	}
	gateway := requested
	if gateway == "" {
		data, _, err = runCommand(ctx, Command{Path: docker, Args: []string{"network", "inspect", "bridge", "--format", "{{json .IPAM.Config}}"}}, transportEnvironment(), "", nil, 32768)
		var networks []struct {
			Gateway string `json:"Gateway"`
		}
		if err != nil || json.Unmarshal(data, &networks) != nil {
			return "", errors.New("Docker bridge gateway discovery failed; configure DockerHostGateway")
		}
		for _, n := range networks {
			if net.ParseIP(n.Gateway) != nil {
				gateway = n.Gateway
				break
			}
		}
	}
	if err = validateBindIP(gateway); err != nil {
		return "", err
	}
	return gateway, nil
}
func (w *nativeWorker) Inspect(ctx context.Context, e domain.Environment) (domain.Inspection, error) {
	bounded, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	home, err := os.UserHomeDir()
	if err != nil {
		return domain.Inspection{}, errors.New("execution-host home directory is unavailable")
	}
	workDir, err := selectedWorkDir(e)
	if err != nil {
		return domain.Inspection{}, err
	}
	result := domain.Inspection{EnvironmentID: e.ID, OS: runtime.GOOS, Arch: runtime.GOARCH, HomeDir: home, WorkDir: workDir, Ready: true, Tools: []domain.ToolStatus{}, Agents: []domain.Agent{}, Issues: []string{}}
	for _, name := range []string{"docker", "uv", "harbor"} {
		status := probeTool(bounded, e, name, []string{"--version"})
		result.Tools = append(result.Tools, status)
		if !status.Available {
			result.Ready = false
			result.Issues = append(result.Issues, name+": "+status.Detail)
		}
	}
	var harbor, docker domain.ToolStatus
	for _, tool := range result.Tools {
		switch tool.Name {
		case "harbor":
			harbor = tool
		case "docker":
			docker = tool
		}
	}
	if harbor.Available {
		if err := checkNativeHarborCapabilities(bounded, e, harbor.Path); err != nil {
			result.Ready = false
			result.Issues = append(result.Issues, err.Error())
		}
		agents, agentErr := discoverAgents(bounded, e, harbor.Path)
		if agentErr != nil {
			result.Ready = false
			result.Issues = append(result.Issues, "Harbor agent capability inspection failed")
		} else {
			result.Agents = agents
		}
	}
	if runtime.GOOS != "linux" {
		result.Ready = false
		result.Issues = append(result.Issues, "first-release SWE-bench native Linux Harbor support is not verified on "+runtime.GOOS+"; use a Linux WSL/SSH worker")
	}
	if docker.Available {
		gateway, dockerErr := inspectDocker(bounded, docker.Path, e.DockerHostGateway)
		if dockerErr != nil {
			result.Ready = false
			result.Issues = append(result.Issues, dockerErr.Error())
		} else {
			result.DockerGateway = gateway
		}
	}
	if bounded.Err() != nil {
		return result, bounded.Err()
	}
	return result, nil
}
func (w *nativeWorker) agentDiscovery(ctx context.Context, e domain.Environment, harbor, version string) ([]domain.Agent, error) {
	key := harbor + "@" + version
	w.mu.Lock()
	if w.agentKey == key && time.Since(w.agentTime) < 5*time.Minute {
		b, _ := json.Marshal(w.agents)
		var agents []domain.Agent
		_ = json.Unmarshal(b, &agents)
		w.mu.Unlock()
		return agents, nil
	}
	w.mu.Unlock()
	agents, err := discoverAgents(ctx, e, harbor)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.agentKey = key
	w.agentTime = time.Now()
	w.agents = agents
	w.mu.Unlock()
	return agents, nil
}
func requireAgent(inspection domain.Inspection, spec domain.ExecutionSpec) (domain.Agent, error) {
	for _, agent := range inspection.Agents {
		if agent.Name == spec.Snapshot.Spec.Agent {
			if !agent.HarborSupported || agent.Command == "" || !containsProtocol(agent.SupportedProtocols, spec.Snapshot.Model.Protocol) {
				return domain.Agent{}, fmt.Errorf("selected agent lacks independent container version observation or %s endpoint override support", spec.Snapshot.Model.Protocol)
			}
			if err := validateAgentKwargs(spec.Snapshot.Spec.AgentKwargs, agent.Schema); err != nil {
				return domain.Agent{}, err
			}
			return agent, nil
		}
	}
	return domain.Agent{}, errors.New("selected agent is not listed by the execution host Harbor adapter registry")
}

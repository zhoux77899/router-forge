package executor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "benchmarks", "harbor", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestNativeAgentCapabilitiesDoNotInferResponsesFromGlobalEnum(t *testing.T) {
	agent, err := ParseAgentCapabilities("opencode", fixture(t, "auth-opencode.json"), fixture(t, "schema-opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !agent.HarborSupported || !agent.SupportsVersion || !reflect.DeepEqual(agent.SupportedProtocols, []domain.Protocol{domain.ChatCompletions, domain.AnthropicMessages}) {
		t.Fatalf("capabilities: %#v", agent)
	}
	if agent.Installed || agent.Version != "" {
		t.Fatal("schema discovery claimed host/container installation")
	}
}
func TestUnsupportedAndMalformedAgentCapabilitiesFailClosed(t *testing.T) {
	agent, err := ParseAgentCapabilities("oracle", []byte(`{"agent":"oracle","supports_model_connection":false,"supports_overriding_base_url":false,"supported_api_formats":[]}`), []byte(`{"type":"object","properties":{}}`))
	if err != nil || len(agent.SupportedProtocols) != 0 || agent.SupportsVersion || agent.Reason == "" {
		t.Fatalf("unmanaged agent capability: %#v %v", agent, err)
	}
	for _, bad := range []string{`{`, `{"agent":"codex","supports_model_connection":true,"supports_overriding_base_url":true,"supported_api_formats":["openai_responses"]}`} {
		if _, err := ParseAgentCapabilities("opencode", []byte(bad), fixture(t, "schema-opencode.json")); err == nil {
			t.Fatalf("accepted mismatched/partial capability %q", bad)
		}
	}
}
func TestWindowsInteropEntriesAreNeverLabeledNativeLinuxExecutables(t *testing.T) {
	for _, p := range []string{"/mnt/c/Users/runner/AppData/Roaming/npm/claude", "/mnt/d/VSCode/bin/copilot", "/home/runner/copilot.exe"} {
		if IsNativeExecutablePath("linux", p) {
			t.Fatalf("interop accepted: %s", p)
		}
	}
	if !IsNativeExecutablePath("linux", "/home/runner/.local/bin/opencode") {
		t.Fatal("valid native path rejected")
	}
}

func TestSuccessfulCustomToolVersionOutputDoesNotNeedNumericVersion(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uv", "harbor"} {
		status := probeTool(context.Background(), domain.Environment{Tools: map[string]string{name: executable}}, name, []string{"--version"})
		if !status.Available || status.Version != "" || status.VersionOutput != "fixture development build" {
			t.Fatalf("custom %s version was incorrectly rejected: %+v", name, status)
		}
	}
}

func TestNativeProtocolAdapterDoesNotRequireVersionInstallationOverride(t *testing.T) {
	agent, err := ParseAgentCapabilities("opencode", fixture(t, "auth-opencode.json"), []byte(`{"type":"object","properties":{}}`))
	if err != nil || agent.SupportsVersion || !agent.HarborSupported || agent.Reason != "" || agent.Command == "" {
		t.Fatalf("default container installation was mistaken for unsupported adapter: %+v %v", agent, err)
	}
}

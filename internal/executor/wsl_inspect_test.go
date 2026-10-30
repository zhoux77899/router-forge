package executor

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestReadOnlyNativeWSLInspection(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("ROUTER_FORGE_READONLY_WSL_INSPECT") != "1" {
		t.Skip("opt-in read-only actual WSL inspection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	inspection, err := newNativeWorker(nil).Inspect(ctx, domain.Environment{ID: "wsl-readonly", Type: "wsl", Distribution: "Ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	result := struct {
		Ready   bool                `json:"ready"`
		OS      string              `json:"os"`
		Arch    string              `json:"arch"`
		Tools   []domain.ToolStatus `json:"tools"`
		Agents  []domain.Agent      `json:"agents"`
		Issues  []string            `json:"issues"`
		Gateway string              `json:"gateway"`
	}{inspection.Ready, inspection.OS, inspection.Arch, inspection.Tools, nil, inspection.Issues, inspection.DockerGateway}
	for _, agent := range inspection.Agents {
		if agent.Name == "opencode" || agent.Name == "claude-code" || agent.Name == "codex" || agent.Name == "copilot-cli" {
			agent.Schema = nil
			result.Agents = append(result.Agents, agent)
		}
	}
	b, _ := json.Marshal(result)
	t.Log(string(b))
	if !inspection.Ready {
		t.Fatal("actual WSL inspection is not ready; see truthful diagnostic above")
	}
}

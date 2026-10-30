package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestHarborTLSBootstrapUsesSameLauncherInterpreterAndArguments(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("requires an installed Python for the owned launcher fixture")
	}
	root := projectTestDir(t)
	launcher := filepath.Join(root, "harbor")
	if err := writePrivate(launcher, []byte("#!"+python+"\nimport json, ssl, sys\nprint(json.dumps({'argv': sys.argv, 'prefix': sys.prefix, 'verify': ssl.create_default_context().verify_mode}))\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(launcher, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "job-config.json")
	e := domain.Environment{SkipTLSVerification: true, Tools: map[string]string{"harbor": launcher, "python": filepath.Join(root, "alternate-python-must-not-run")}}
	command, err := harborRunCommand(e, launcher, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, _, err := runCommand(ctx, command, nativeProcessEnv(e, root, ""), "", nil, 8192)
	if err != nil {
		t.Fatal("the explicit TLS bootstrap could not execute its original launcher", err)
	}
	var observed struct {
		Argv   []string `json:"argv"`
		Prefix string   `json:"prefix"`
		Verify int      `json:"verify"`
	}
	if err := json.Unmarshal(data, &observed); err != nil {
		t.Fatal(err, string(data))
	}
	prefix, _, err := runCommand(ctx, Command{Path: python, Args: []string{"-I", "-c", "import sys; print(sys.prefix)"}}, transportEnvironment(), "", nil, 8192)
	if err != nil || observed.Prefix != strings.TrimSpace(string(prefix)) || observed.Verify != 0 || strings.Join(observed.Argv, "\n") != strings.Join([]string{launcher, "run", "--config", config}, "\n") {
		t.Fatal("TLS bootstrap changed the native interpreter/venv/launcher or arguments", observed, err)
	}
	e.SkipTLSVerification = false
	strict, err := harborRunCommand(e, launcher, config)
	if err != nil || strict.Path != launcher || strings.Join(strict.Args, "\n") != "run\n--config\n"+config {
		t.Fatal("strict TLS did not restore the original native CLI command", strict, err)
	}
}

func TestContainerTLSSiteCustomizationFailsClosedInsteadOfIgnoringUnsupportedPolicy(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("requires installed Python")
	}
	root := projectTestDir(t)
	if err := writeTLSProfile(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tlsProfileDir(root), "python", "router_forge_tls.py")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, code, err := runCommand(ctx, Command{Path: python, Args: []string{"-c", "print('must-not-run')"}}, nativeProcessEnv(domain.Environment{SkipTLSVerification: true}, root, ""), "", nil, 1024)
	if err == nil || code != 78 || len(data) != 0 {
		t.Fatal("Python silently ignored a requested TLS policy instead of failing before client work", code, err, string(data))
	}
}

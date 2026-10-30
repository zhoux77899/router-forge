package executor

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerCancellationNeverRemovesUnownedContainers(t *testing.T) {
	spec := fixtureSpec()
	root := projectTestDir(t)
	owned := DockerContainer{ID: strings.Repeat("a", 64), Image: "sha256:" + strings.Repeat("1", 64), Labels: map[string]string{"io.router-forge.owner": "owner_fixture", "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}}
	foreign := DockerContainer{ID: strings.Repeat("b", 64), Image: owned.Image, Labels: map[string]string{"io.router-forge.owner": "foreign", "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}}
	tracker := newDockerTracker("fixture-docker", nil, "owner_fixture", root, filepath.Join(root, "jobs"), spec)
	removed := []string{}
	tracker.runner = func(ctx context.Context, c Command, env []string, dir string, in io.Reader, limit int) ([]byte, int, error) {
		switch c.Args[0] {
		case "ps":
			return []byte(owned.ID + "\n" + foreign.ID + "\n"), 0, nil
		case "inspect":
			value := owned
			if c.Args[1] == foreign.ID {
				value = foreign
			}
			b, _ := json.Marshal(value)
			return b, 0, nil
		case "network":
			return nil, 0, nil
		case "rm":
			removed = append(removed, c.Args[len(c.Args)-1])
			return nil, 0, nil
		}
		t.Fatalf("unexpected Docker cleanup command: %#v", c)
		return nil, 1, nil
	}
	tracker.cleanup()
	if !reflect.DeepEqual(removed, []string{owned.ID}) {
		t.Fatalf("removed resources = %v", removed)
	}
	if OwnsContainer(foreign, "owner_fixture", spec.RunID, spec.AttemptID) {
		t.Fatal("foreign owner accepted")
	}
}
func TestHarborProcessEnvironmentHasNoInheritedProviderOrSSHAuth(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-host-key-never-forward")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "synthetic-host-auth-never-forward")
	t.Setenv("SSH_AUTH_SOCK", "synthetic-host-agent-socket")
	root := projectTestDir(t)
	spec := fixtureSpec()
	env := nativeProcessEnv(spec.Snapshot.Environment, root, "fixture-lease")
	joined := strings.Join(env, "\n")
	for _, bad := range []string{"synthetic-host-key", "synthetic-host-auth", "synthetic-host-agent-socket"} {
		if strings.Contains(joined, bad) {
			t.Fatal("host auth forwarded")
		}
	}
	if !strings.Contains(joined, "HOME="+filepath.Join(root, "home")) || !strings.Contains(joined, attemptKeyName+"=fixture-lease") {
		t.Fatal("isolated home or scoped lease missing")
	}
	if _, err := reserveAttempt(root, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveAttempt(root, spec); err == nil {
		t.Fatal("existing attempt reused")
	}
}
func TestNativeCAMountAlsoReachesSeparateVerifierViaConstructorKwargs(t *testing.T) {
	spec := fixtureSpec()
	spec.Snapshot.Environment.CACertPath = "/home/runner/ca-profile.pem"
	spec.Snapshot.Environment.ProxyURL = "http://proxy.example:3128"
	spec.Snapshot.Environment.NoProxy = "registry.example"
	spec.Snapshot.Environment.DockerHostGateway = "172.17.0.1"
	sealFixtureSpec(&spec)
	cfg, err := BuildHarborJobConfig(spec, domainTask(spec), "http://router-forge-gateway:4567", "/home/runner/work/jobs", "/home/runner/work/compose.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environment.Mounts) != 1 || !cfg.Environment.Mounts[0].ReadOnly || cfg.Environment.Mounts[0].Source != "/home/runner/ca-profile.pem" || cfg.Environment.Mounts[0].Target != "/etc/router-forge/ca.pem" {
		t.Fatal("read-only native CA mount missing")
	}
	if cfg.Environment.Kwargs["mounts"] == nil {
		t.Fatal("separate verifier constructor CA mount missing")
	}
	for _, env := range []map[string]string{cfg.Agents[0].Env, cfg.Environment.Env, cfg.Verifier.Env} {
		if env["SSL_CERT_FILE"] != "/etc/router-forge/ca.pem" || env["NODE_EXTRA_CA_CERTS"] != "/etc/router-forge/ca.pem" || env["GIT_SSL_CAINFO"] != "/etc/router-forge/ca.pem" || !strings.Contains(env["NO_PROXY"], "router-forge-gateway,172.17.0.1") {
			t.Fatalf("container profile = %#v", env)
		}
	}
	host := strings.Join(nativeProcessEnv(spec.Snapshot.Environment, projectTestDir(t), ""), "\n")
	if !strings.Contains(host, "GIT_SSL_CAINFO="+spec.Snapshot.Environment.CACertPath) || strings.Contains(host, "GIT_SSL_CAINFO=/etc/router-forge/ca.pem") {
		t.Fatal("native Git must use the approved host CA path rather than the container mount path")
	}
	if _, present := profileEnv(fixtureSpec().Snapshot.Environment, "", "", false)["GIT_SSL_CAINFO"]; present {
		t.Fatal("Git trust must not be overridden when no CA profile is approved")
	}
}

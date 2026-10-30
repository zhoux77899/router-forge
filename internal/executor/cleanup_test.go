package executor

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCleanupIncludesPrivateNativeVerifierButNeverForeignContainersOrNetworks(t *testing.T) {
	root := projectTestDir(t)
	spec := fixtureSpec()
	jobs := filepath.Join(root, "jobs")
	trial := spec.Snapshot.Spec.TaskNames[0] + "__fixturex"
	if err := os.MkdirAll(filepath.Join(jobs, trial), 0700); err != nil {
		t.Fatal(err)
	}
	project := trial + "__env"
	verifier := trial + "__verifier__fixturey"
	owner := "fixture_owner"
	labels := map[string]string{"io.router-forge.owner": owner, "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID, "com.docker.compose.project": project, "com.docker.compose.project.working_dir": root}
	container := func(id string, l map[string]string) DockerContainer {
		return DockerContainer{ID: strings.Repeat(id, 64), Image: "sha256:" + strings.Repeat("1", 64), Labels: l}
	}
	main := container("1", labels)
	sidecar := container("2", map[string]string{"com.docker.compose.project": project, "com.docker.compose.project.working_dir": root})
	verify := container("3", map[string]string{"com.docker.compose.project": verifier, "com.docker.compose.project.working_dir": root})
	foreign := container("4", map[string]string{"io.router-forge.owner": "another-owner", "com.docker.compose.project": project, "com.docker.compose.project.working_dir": root})
	outside := container("5", map[string]string{"com.docker.compose.project": project, "com.docker.compose.project.working_dir": filepath.Dir(root)})
	all := map[string]DockerContainer{main.ID: main, sidecar.ID: sidecar, verify.ID: verify, foreign.ID: foreign, outside.ID: outside}
	networkOwned := strings.Repeat("a", 64)
	networkForeign := strings.Repeat("b", 64)
	networkBusy := strings.Repeat("c", 64)
	removed := []string{}
	removedNetworks := []string{}
	tracker := newDockerTracker("fixture-docker", nil, owner, root, jobs, spec)
	tracker.runner = func(ctx context.Context, c Command, env []string, dir string, in io.Reader, limit int) ([]byte, int, error) {
		switch c.Args[0] {
		case "ps":
			joined := strings.Join(c.Args, " ")
			if strings.Contains(joined, "--format") {
				return []byte(verifier + "\nunrelated__verifier__x"), 0, nil
			}
			ids := []string{main.ID, foreign.ID}
			if strings.Contains(joined, "label=com.docker.compose.project="+project) {
				ids = []string{main.ID, sidecar.ID, foreign.ID, outside.ID}
			} else if strings.Contains(joined, "label=com.docker.compose.project="+verifier) {
				ids = []string{verify.ID}
			}
			return []byte(strings.Join(ids, "\n")), 0, nil
		case "inspect":
			data, _ := json.Marshal(all[c.Args[1]])
			return data, 0, nil
		case "rm":
			removed = append(removed, c.Args[len(c.Args)-1])
			return nil, 0, nil
		case "network":
			switch c.Args[1] {
			case "ls":
				return []byte(networkOwned + "\n" + networkForeign + "\n" + networkBusy), 0, nil
			case "inspect":
				id := c.Args[2]
				l := labels
				containers := map[string]any{}
				if id == networkForeign {
					l = map[string]string{"io.router-forge.owner": "another-owner"}
				}
				if id == networkBusy {
					containers[foreign.ID] = map[string]any{}
				}
				data, _ := json.Marshal(map[string]any{"id": id, "labels": l, "containers": containers})
				return data, 0, nil
			case "rm":
				removedNetworks = append(removedNetworks, c.Args[2])
				return nil, 0, nil
			}
		}
		t.Fatalf("unexpected scoped Docker command: %#v", c)
		return nil, 1, nil
	}
	if tracker.cleanup() == nil {
		t.Fatal("busy owned network was not reported honestly")
	}
	sort.Strings(removed)
	if !reflect.DeepEqual(removed, []string{main.ID, sidecar.ID, verify.ID}) {
		t.Fatalf("removed containers=%v", removed)
	}
	if !reflect.DeepEqual(removedNetworks, []string{networkOwned}) {
		t.Fatalf("removed networks=%v", removedNetworks)
	}
}

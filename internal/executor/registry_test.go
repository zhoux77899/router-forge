package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func registryFixture(t *testing.T, version string, tasks []registryTask) []byte {
	t.Helper()
	data, err := json.Marshal([]registryDataset{{Name: domain.SWEBenchVerified().Dataset, Version: version, Tasks: tasks}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRegistryExplicitVersionAndGeneralGitSources(t *testing.T) {
	suite := domain.SWEBenchVerified()
	suite.Version, suite.TaskCount = "fixture-r7", 999
	data := registryFixture(t, suite.Version, []registryTask{
		{Name: "fixture__task-2", Path: "tasks/group/fixture__task-2", GitURL: "https://code.fixture.test/one.git", GitCommit: strings.Repeat("A", 40)},
		{Name: "fixture__task-1", Path: "alternative/fixture__task-1", GitURL: "https://code.fixture.test/two.git", GitCommit: strings.Repeat("b", 64)},
	})
	tasks, err := ParseRegistry(data, suite)
	if err != nil || len(tasks) != 2 || tasks[0].Name != "fixture__task-1" || tasks[1].GitCommit != strings.Repeat("a", 40) || tasks[1].Kind != "git" {
		t.Fatalf("tasks = %#v, %v", tasks, err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), strings.Repeat("A", 40), "main", 1),
		strings.Replace(string(data), "tasks/group/fixture__task-2", "../../fixture__task-2", 1),
		strings.ReplaceAll(string(data), "fixture__task-2", "fixture__task-1"),
		strings.Replace(string(data), "https://code.fixture.test/one.git", "https://token@code.fixture.test/one.git", 1),
		`[`,
	} {
		if _, err := ParseRegistry([]byte(bad), suite); err == nil {
			t.Fatal("accepted unsafe, duplicate or unresolved registry task")
		}
	}
	suite.Version = ""
	if _, err := ParseRegistry(data, suite); err == nil {
		t.Fatal("legacy parser selected a homemade default version")
	}
	suite.Version = "fixture-r8"
	if _, err := ParseRegistry(data, suite); err == nil {
		t.Fatal("legacy parser substituted another version")
	}
}

func TestRegistryRejectsAmbiguousVersion(t *testing.T) {
	suite := domain.SWEBenchVerified()
	suite.Version = "fixture-r4"
	entry := registryDataset{Name: suite.Dataset, Version: suite.Version, Tasks: []registryTask{{Name: "fixture", Path: "any/fixture", GitURL: "https://code.fixture.test/tasks.git", GitCommit: strings.Repeat("b", 40)}}}
	data, _ := json.Marshal([]registryDataset{entry, entry})
	if _, err := ParseRegistry(data, suite); err == nil {
		t.Fatal("accepted ambiguous dataset metadata")
	}
}

func TestNativeSuiteIdentityHasNoSourceVersionPins(t *testing.T) {
	suite := domain.SWEBenchVerified()
	suite.Version, suite.RegistryURL, suite.TaskRepository, suite.TaskCommit, suite.TaskCount = "arbitrary-observed-version", "https://native.fixture.test/registry", "https://code.fixture.test/tasks.git", strings.Repeat("c", 40), 3
	if err := validateSuite(suite); err != nil {
		t.Fatal(err)
	}
	for _, dataset := range []string{"swebench-verified", "other/swe-bench-verified", "swe-bench/swe-bench-verified@latest"} {
		suite.Dataset = dataset
		if err := validateSuite(suite); err == nil {
			t.Fatalf("accepted alternate dataset %q", dataset)
		}
	}
}

func TestProfileCannotDisableTLSOrAcceptInvalidCA(t *testing.T) {
	if _, err := profileClient(domain.Environment{ProxyURL: "file:///etc/proxy"}); err == nil {
		t.Fatal("accepted non-network proxy")
	}
	dir := projectTestDir(t)
	path := filepath.Join(dir, "invalid-ca.pem")
	if err := writePrivate(path, []byte("not a certificate")); err != nil {
		t.Fatal(err)
	}
	if _, err := profileClient(domain.Environment{CACertPath: path}); err == nil {
		t.Fatal("invalid CA silently bypassed verification")
	}
}

func TestRegistryAcceptsMetadataBeyondEightMiB(t *testing.T) {
	suite := domain.SWEBenchVerified()
	suite.Version = "fixture-r6"
	var tasks []registryTask
	for i := range 500 {
		name := fmt.Sprintf("fixture__task-%03d", i)
		tasks = append(tasks, registryTask{Name: name, Path: "tasks/" + name, GitURL: "https://code.fixture.test/tasks.git", GitCommit: strings.Repeat("d", 40)})
	}
	data := append([]byte(strings.Repeat(" ", 13<<20)), registryFixture(t, suite.Version, tasks)...)
	selected, err := ParseRegistry(data, suite)
	if err != nil || len(selected) != 500 || selected[0].Name != "fixture__task-000" {
		t.Fatalf("bounded full registry: count=%d error=%v", len(selected), err)
	}
}

func TestRegistryRejectsMetadataBeyondThirtyTwoMiB(t *testing.T) {
	suite := domain.SWEBenchVerified()
	suite.Version = "fixture-r6"
	if _, err := ParseRegistry([]byte(strings.Repeat(" ", 33<<20)), suite); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatal("oversized registry exceeded its parsing limit")
	}
	client := &http.Client{Transport: registryTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", 33<<20))), Header: http.Header{}}, nil
	})}
	if _, err := fetchPublic(context.Background(), client, "https://native.fixture.test/metadata", maxRegistryBytes); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatal("oversized public metadata exceeded its fetch limit")
	}
}

func TestNativeTasksNeverFallsBackToLegacyMetadata(t *testing.T) {
	worker := &nativeWorker{clientFactory: func(domain.Environment) (*http.Client, error) {
		t.Fatal("native resolver fell back to manually fetched registry metadata")
		return nil, nil
	}}
	environment := domain.Environment{Tools: map[string]string{"harbor": filepath.Join(projectTestDir(t), "missing-harbor")}}
	if _, err := worker.Tasks(context.Background(), environment, domain.SWEBenchVerified()); err == nil {
		t.Fatal("missing installed Harbor did not fail explicitly")
	}
}

type registryTestTransport func(*http.Request) (*http.Response, error)

func (f registryTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

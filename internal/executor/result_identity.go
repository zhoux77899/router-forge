package executor

import (
	"encoding/json"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func trialDirectoryPrefix(task string) string {
	if simpleName.MatchString(task) {
		return task
	}
	return ""
}

func verifyHarborTaskSource(data []byte, source domain.BenchmarkTask) error {
	var result struct {
		Config struct {
			Task *HarborTaskConfig `json:"task"`
		} `json:"config"`
	}
	if json.Unmarshal(data, &result) != nil {
		return ErrIncompleteResult
	}
	reference := result.Config.Task
	if reference == nil {
		return ErrResultIdentity
	}
	if source.Kind == "package" {
		if reference.Name != source.PackageName || reference.Ref != source.Ref || reference.Path != "" || reference.GitURL != "" || reference.GitCommitID != "" {
			return ErrResultIdentity
		}
	} else if reference.Path != source.Path || reference.GitURL != source.GitURL || reference.GitCommitID != source.GitCommit || reference.Name != "" || reference.Ref != "" {
		return ErrResultIdentity
	}
	return nil
}

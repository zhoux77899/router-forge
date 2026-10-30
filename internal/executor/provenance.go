package executor

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func finalizeProvenance(path string, record map[string]any, snapshot domain.RunSnapshot, result domain.ExecutionResult) error {
	copy, err := domain.CloneSnapshot(snapshot)
	if err != nil {
		return errors.New("could not clone final acknowledged execution provenance")
	}
	runtime := copy.Runtime
	record["runtime"] = runtime
	record["agent_version"] = ""
	if runtime != nil {
		record["agent_version"] = runtime.AgentVersion
	}
	record["image_digests"] = append([]string{}, result.ImageDigests...)
	record["execution_state"] = result.State
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return errors.New("could not encode final acknowledged execution provenance")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("initial private execution provenance is missing or unsafe")
	}
	staging := path + "." + domain.NewID("final")
	if err := writePrivate(staging, data); err != nil {
		return errors.New("could not persist final acknowledged execution provenance")
	}
	defer os.Remove(staging)
	if err := os.Rename(staging, path); err != nil {
		return errors.New("could not replace private execution provenance with final observations")
	}
	return nil
}

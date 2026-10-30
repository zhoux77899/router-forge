package executor

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func setNetworkProvenance(record map[string]any, snapshot domain.RunSnapshot) {
	if snapshot.Network == nil {
		record["skip_tls_verification"] = snapshot.Environment.SkipTLSVerification
		record["tls_verification_scope"] = "harbor_and_runtime_container_clients"
		return
	}
	delete(record, "skip_tls_verification")
	delete(record, "tls_verification_scope")
	record["network"] = snapshot.Network
	record["network_evidence_kind"] = "frozen_configuration_not_connection_success"
	record["verifier_tls_verification"] = "verify"
}

func finalizeProvenance(path string, record map[string]any, snapshot domain.RunSnapshot, result domain.ExecutionResult) error {
	copy, err := domain.CloneSnapshot(snapshot)
	if err != nil {
		return errors.New("could not clone final acknowledged execution provenance")
	}
	runtime := copy.Runtime
	setNetworkProvenance(record, copy)
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
	return replaceOwnedProvenance(path, data)
}

func replaceOwnedProvenance(path string, data []byte) error {
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

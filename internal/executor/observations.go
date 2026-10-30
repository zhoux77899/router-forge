package executor

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func recordObservation(snapshot *domain.RunSnapshot, observe func(domain.ExecutionObservation) error, value domain.ExecutionObservation) error {
	if observe == nil {
		return errors.New("durable execution observation sink is required")
	}
	candidate, err := domain.CloneSnapshot(*snapshot)
	if err != nil {
		return err
	}
	if err := candidate.ApplyObservation(value); err != nil {
		return err
	}
	if err := observe(value); err != nil {
		return fmt.Errorf("execution observation was not durably acknowledged; inference remains blocked: %w", err)
	}
	*snapshot = candidate
	return nil
}

func observedAgentVersion(data []byte) (string, string) {
	output := strings.TrimSpace(ansiEscape.ReplaceAllString(string(data), ""))
	if output == "" || len(output) > 32768 || !utf8.ValidString(output) || strings.ContainsRune(output, 0) {
		return "", ""
	}
	text := strings.Join(strings.Fields(output), " ")
	lower := strings.ToLower(text)
	for _, unavailable := range []string{"unknown", "unavailable", "error", "usage:", "not found"} {
		if strings.HasPrefix(lower, unavailable) || strings.Contains(lower, "command not found") || strings.Contains(lower, ": not found") || strings.Contains(lower, "no such file or directory") {
			return "", output
		}
	}
	version := strings.TrimPrefix(versionPattern.FindString(output), "v")
	if version == "" {
		version = text
		if len(version) > 256 {
			return "", output
		}
	}
	return version, output
}

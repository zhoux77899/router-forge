package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type taskMetadata struct {
	Image              string
	VerifierTimeoutSec int
	BuildTimeoutSec    int
	VerifierMode       string
	VerifierImage      string
}

func parseTaskMetadata(data []byte) (taskMetadata, error) {
	result := taskMetadata{VerifierTimeoutSec: 600, BuildTimeoutSec: 600, VerifierMode: "shared"}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	section := ""
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return result, errors.New("invalid original task metadata section")
			}
			section = strings.Trim(line, "[] ")
			if section == "verifier.environment" {
				result.VerifierMode = "separate"
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		identity := section + "." + key
		switch identity {
		case "environment.docker_image", "environment.build_timeout_sec", "verifier.timeout_sec", "verifier.environment_mode", "verifier.environment.docker_image":
		default:
			continue
		}
		if seen[identity] {
			return result, errors.New("duplicate original task environment metadata")
		}
		seen[identity] = true
		switch identity {
		case "environment.docker_image", "verifier.environment.docker_image", "verifier.environment_mode":
			parsed, err := tomlString(value)
			if err != nil {
				return result, err
			}
			switch identity {
			case "environment.docker_image":
				result.Image = parsed
			case "verifier.environment.docker_image":
				result.VerifierImage = parsed
			case "verifier.environment_mode":
				if parsed != "shared" && parsed != "separate" && parsed != "auto" {
					return result, errors.New("unsupported verifier environment mode")
				}
				if parsed != "auto" {
					result.VerifierMode = parsed
				}
			}
		default:
			value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || number < 1 || number > 86400 {
				return result, errors.New("original task timeout is outside supported limits")
			}
			if identity == "environment.build_timeout_sec" {
				result.BuildTimeoutSec = int(number)
			} else {
				result.VerifierTimeoutSec = int(number)
			}
		}
	}
	if scanner.Err() != nil {
		return result, errors.New("original task metadata exceeded its line limit")
	}
	return result, nil
}
func tomlString(value string) (string, error) {
	if len(value) == 0 {
		return "", errors.New("empty TOML string")
	}
	quote := value[0]
	if quote != '\'' && quote != '"' {
		return "", errors.New("only literal original task metadata strings are supported")
	}
	escaped := false
	end := -1
	for i := 1; i < len(value); i++ {
		if quote == '"' && value[i] == '\\' && !escaped {
			escaped = true
			continue
		}
		if value[i] == quote && !escaped {
			end = i
			break
		}
		escaped = false
	}
	if end < 0 {
		return "", errors.New("unterminated task metadata string")
	}
	trailing := strings.TrimSpace(value[end+1:])
	if trailing != "" && !strings.HasPrefix(trailing, "#") {
		return "", errors.New("unsupported task metadata string expression")
	}
	if quote == '\'' {
		return value[1:end], nil
	}
	parsed, err := strconv.Unquote(value[:end+1])
	if err != nil {
		return "", errors.New("invalid task metadata string escape")
	}
	return parsed, nil
}

var dockerReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,511}$`)
var imageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var repoDigestPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:+-]*@sha256:[a-f0-9]{64}$`)
var fromInstruction = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)(?:\s+AS\s+([A-Za-z0-9_.-]+))?\s*(?:#.*)?$`)

type frozenImage struct {
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
	ID        string `json:"id"`
}

func dockerImageIdentity(ctx context.Context, docker, reference string, env []string) (frozenImage, error) {
	if !dockerReference.MatchString(reference) {
		return frozenImage{}, errors.New("invalid original task Docker image reference")
	}
	inspect := func() (frozenImage, error) {
		data, _, err := runCommand(ctx, Command{Path: docker, Args: []string{"image", "inspect", reference, "--format", `{"id":{{json .Id}},"repo_digests":{{json .RepoDigests}}}`}}, env, "", nil, 128<<10)
		var value struct {
			ID          string   `json:"id"`
			RepoDigests []string `json:"repo_digests"`
		}
		if err != nil || json.Unmarshal(data, &value) != nil || !imageIDPattern.MatchString(value.ID) {
			return frozenImage{}, errors.New("original task image is not available locally")
		}
		var digest string
		for _, candidate := range value.RepoDigests {
			if repoDigestPattern.MatchString(candidate) {
				digest = candidate
				break
			}
		}
		if digest == "" && repoDigestPattern.MatchString(reference) {
			digest = reference
		}
		if digest == "" {
			digest = value.ID
		}
		return frozenImage{Reference: reference, Digest: digest, ID: value.ID}, nil
	}
	if image, err := inspect(); err == nil {
		return image, nil
	}
	if _, _, err := runCommand(ctx, Command{Path: docker, Args: []string{"pull", reference}}, env, "", nil, 1<<20); err != nil {
		return frozenImage{}, errors.New("could not resolve the original task image using existing Docker/proxy/CA configuration")
	}
	return inspect()
}

// FreezeDockerfile pins only base image references, preserving the native task build and verifier.
func FreezeDockerfile(data []byte, resolve func(string) (string, error)) ([]byte, []string, error) {
	if len(data) > 1<<20 || resolve == nil {
		return nil, nil, errors.New("invalid bounded original task Dockerfile")
	}
	lines := strings.Split(string(data), "\n")
	stages := map[string]bool{}
	digests := []string{}
	fromCount := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "FROM ") {
			match := fromInstruction.FindStringSubmatchIndex(line)
			if match == nil {
				return nil, nil, errors.New("task Dockerfile FROM expression cannot be safely frozen")
			}
			reference := line[match[2]:match[3]]
			if strings.Contains(reference, "$") {
				return nil, nil, errors.New("dynamic Dockerfile base image arguments cannot be frozen in this release")
			}
			if !stages[strings.ToLower(reference)] && strings.ToLower(reference) != "scratch" {
				if !dockerReference.MatchString(reference) {
					return nil, nil, errors.New("unsafe original Dockerfile image reference")
				}
				digest, err := resolve(reference)
				if err != nil {
					return nil, nil, err
				}
				if !repoDigestPattern.MatchString(digest) {
					return nil, nil, errors.New("native build base image requires a repository digest")
				}
				lines[i] = line[:match[2]] + digest + line[match[3]:]
				digests = append(digests, digest)
			}
			if match[4] >= 0 {
				stages[strings.ToLower(line[match[4]:match[5]])] = true
			}
			fromCount++
		}
		if strings.Contains(strings.ToLower(trimmed), "--from=") {
			for _, part := range strings.Fields(trimmed) {
				if strings.HasPrefix(strings.ToLower(part), "--from=") {
					source := strings.TrimPrefix(part, "--from=")
					if !stages[strings.ToLower(source)] {
						if _, err := strconv.Atoi(source); err != nil {
							return nil, nil, errors.New("external COPY image references require an unsupported build pinning mode")
						}
					}
				}
			}
		}
	}
	if fromCount == 0 {
		return nil, nil, errors.New("task Dockerfile contains no native base image")
	}
	return []byte(strings.Join(lines, "\n")), digests, nil
}
func taskImageTag(spec domain.ExecutionSpec) string {
	return "router-forge/" + strings.ToLower(spec.AttemptID) + ":task"
}
func composeOverlay(spec domain.ExecutionSpec, owner, bindIP, image, pinnedDockerfile string) ([]byte, error) {
	labels := map[string]string{"io.router-forge.owner": owner, "io.router-forge.run": spec.RunID, "io.router-forge.attempt": spec.AttemptID}
	main := map[string]any{"labels": labels, "extra_hosts": map[string]string{"router-forge-gateway": bindIP}, "image": image}
	if pinnedDockerfile != "" {
		main["build"] = map[string]string{"dockerfile": pinnedDockerfile}
	}
	data, err := json.MarshalIndent(map[string]any{"services": map[string]any{"main": main}, "networks": map[string]any{"default": map[string]any{"labels": labels}}}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not encode native Docker overlay")
	}
	return data, nil
}

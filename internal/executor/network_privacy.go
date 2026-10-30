package executor

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

var proxyUserinfo = regexp.MustCompile(`(?i)(https?://)[^\s/]*@`)
var proxyAuthorization = regexp.MustCompile(`(?i)\b(?:proxy-authorization|authorization)\s*[:=]\s*(?:basic|bearer)\s+[^\s,;]+`)

func redactCommandText(message string, command workerCommand) string {
	credentials := []domain.ProxyCredential{}
	if command.NetworkRuntime != nil {
		for _, credential := range command.NetworkRuntime.Credentials {
			credentials = append(credentials, credential)
		}
	}
	if command.Spec != nil {
		if command.Spec.GatewayToken != "" {
			message = strings.ReplaceAll(message, command.Spec.GatewayToken, "[redacted]")
		}
		if command.Spec.Networks != nil {
			for _, credential := range command.Spec.Networks.Credentials {
				credentials = append(credentials, credential)
			}
		}
	}
	if command.NetworkOperation != nil && command.NetworkOperation.Credential != nil {
		credentials = append(credentials, *command.NetworkOperation.Credential)
	}
	for _, credential := range credentials {
		for _, value := range []string{credential.Password, credential.Username, base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password)), url.QueryEscape(credential.Password), url.PathEscape(credential.Password)} {
			if value != "" {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	message = proxyUserinfo.ReplaceAllString(message, "${1}[redacted]@")
	message = proxyAuthorization.ReplaceAllString(message, "[redacted authorization]")
	return message
}

func privateToolStatuses(tools []domain.ToolStatus, redact func(string) string) []domain.ToolStatus {
	copied := append([]domain.ToolStatus(nil), tools...)
	for i := range copied {
		copied[i].VersionOutput = redact(copied[i].VersionOutput)
		copied[i].Version = redact(copied[i].Version)
		copied[i].Detail = redact(copied[i].Detail)
	}
	return copied
}

func privateInspection(value domain.Inspection, redact func(string) string) domain.Inspection {
	value.Tools = privateToolStatuses(value.Tools, redact)
	value.Issues = append([]string(nil), value.Issues...)
	for i := range value.Issues {
		value.Issues[i] = redact(value.Issues[i])
	}
	value.Agents = append([]domain.Agent(nil), value.Agents...)
	for i := range value.Agents {
		agent := &value.Agents[i]
		agent.Version, agent.Reason = redact(agent.Version), redact(agent.Reason)
		if raw := string(agent.Schema); redact(raw) != raw {
			agent.Schema, agent.HarborSupported = nil, false
			agent.Reason = "native capability schema contained private data and was rejected"
			value.Ready = false
			value.Issues = append(value.Issues, agent.Reason)
		}
	}
	if value.DockerNetwork != nil {
		copied := *value.DockerNetwork
		copied.HTTPProxy, copied.HTTPSProxy, copied.NoProxy = redact(copied.HTTPProxy), redact(copied.HTTPSProxy), redact(copied.NoProxy)
		copied.Mirrors = append([]string(nil), copied.Mirrors...)
		for i := range copied.Mirrors {
			copied.Mirrors[i] = redact(copied.Mirrors[i])
		}
		value.DockerNetwork = &copied
	}
	return value
}

func privateExecutionResult(value domain.ExecutionResult, redact func(string) string) domain.ExecutionResult {
	value.Error, value.AgentVersion = redact(value.Error), redact(value.AgentVersion)
	value.ExceptionType = redact(value.ExceptionType)
	return value
}

func privateObservation(spec *domain.ExecutionSpec, value domain.ExecutionObservation) domain.ExecutionObservation {
	command := workerCommand{Spec: spec}
	value.AgentVersionOutput = redactCommandText(value.AgentVersionOutput, command)
	value.AgentVersion = redactCommandText(value.AgentVersion, command)
	value.Tools = privateToolStatuses(value.Tools, func(text string) string { return redactCommandText(text, command) })
	return value
}

func recordPrivateObservation(spec *domain.ExecutionSpec, observe func(domain.ExecutionObservation) error, value domain.ExecutionObservation) error {
	return recordObservation(&spec.Snapshot, observe, privateObservation(spec, value))
}

func redactCommandMessage(message string, command workerCommand) string {
	return redactedMessage(redactCommandText(message, command), "")
}

package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"unicode"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type dockerNetworkInfo struct {
	OSType         string `json:"OSType"`
	HTTPProxy      string `json:"HTTPProxy"`
	HTTPSProxy     string `json:"HTTPSProxy"`
	NoProxy        string `json:"NoProxy"`
	RegistryConfig struct {
		Mirrors []string `json:"Mirrors"`
	} `json:"RegistryConfig"`
}

func observedDockerNetwork(info dockerNetworkInfo) *domain.DockerNetworkObservation {
	result := &domain.DockerNetworkObservation{Mirrors: []string{}, HTTPProxy: redactObservedURL(info.HTTPProxy), HTTPSProxy: redactObservedURL(info.HTTPSProxy), ObservedAt: domain.Now()}
	for _, mirror := range info.RegistryConfig.Mirrors {
		if len(result.Mirrors) >= 128 {
			break
		}
		result.Mirrors = append(result.Mirrors, redactObservedURL(mirror))
	}
	noProxy := info.NoProxy
	if len(noProxy) > 8192 || strings.IndexFunc(noProxy, unicode.IsControl) >= 0 || strings.ContainsAny(noProxy, "@=;?#\\") {
		noProxy = "[redacted]"
	}
	result.NoProxy = noProxy
	return result
}
func inspectDockerNetwork(ctx context.Context, docker, mode, requested string) (string, *domain.DockerNetworkObservation, error) {
	return inspectDockerNetworkWith(ctx, docker, mode, requested, runCommand, validateBindIP)
}

func inspectDockerNetworkWith(ctx context.Context, docker, mode, requested string, runner commandRunner, validateBind func(string) error) (string, *domain.DockerNetworkObservation, error) {
	if mode != "detect" && mode != "manual" {
		return "", nil, errors.New("explicitly select detected or manual Docker gateway mode")
	}
	if mode == "manual" && requested == "" {
		return "", nil, errors.New("manual Docker gateway mode requires a selected host address")
	}
	data, _, err := runner(ctx, Command{Path: docker, Args: []string{"info", "--format", "{{json .}}"}}, transportEnvironment(), "", nil, 128<<10)
	var info dockerNetworkInfo
	if err != nil || json.Unmarshal(data, &info) != nil {
		return "", nil, errors.New("Docker daemon metadata is unavailable using existing user permissions")
	}
	observation := observedDockerNetwork(info)
	if info.OSType != "linux" {
		return "", observation, errors.New("SWE-bench requires native Linux task containers")
	}
	gateway := requested
	if mode == "detect" {
		gateway = ""
		data, _, err = runner(ctx, Command{Path: docker, Args: []string{"network", "inspect", "bridge", "--format", "{{json .IPAM.Config}}"}}, transportEnvironment(), "", nil, 32768)
		var networks []struct {
			Gateway string `json:"Gateway"`
		}
		if err != nil || json.Unmarshal(data, &networks) != nil {
			return "", observation, errors.New("selected Docker bridge gateway detection failed")
		}
		for _, entry := range networks {
			if net.ParseIP(entry.Gateway) != nil {
				gateway = entry.Gateway
				break
			}
		}
	}
	if err := validateBind(gateway); err != nil {
		return "", observation, err
	}
	return gateway, observation, nil
}

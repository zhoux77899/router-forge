package executor

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type Command struct {
	Path string
	Args []string
}

var simpleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var sshHost = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9_.-]*|\[[0-9A-Fa-f:]+\]|[0-9A-Fa-f:]+)$`)
var sshUser = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

func validText(s string) bool {
	if s == "" || len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func psQuote(s string) string    { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func encodedPowerShell(script string) string {
	runes := utf16.Encode([]rune(script))
	b := make([]byte, len(runes)*2)
	for i, r := range runes {
		b[2*i] = byte(r)
		b[2*i+1] = byte(r >> 8)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

func WorkerFilename(goos, arch string) (string, error) {
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return "", fmt.Errorf("unsupported worker OS %q", goos)
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported worker architecture %q", arch)
	}
	name := "router-forge-worker-" + goos + "-" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

func validateEnvironment(e domain.Environment) error {
	switch e.Type {
	case "local":
	case "wsl":
		if runtime.GOOS != "windows" {
			return errors.New("WSL execution is supported only on a Windows host")
		}
		if !validText(e.Distribution) || strings.HasPrefix(e.Distribution, "-") || strings.ContainsAny(e.Distribution, `\/;|&<>\"'`) {
			return errors.New("invalid WSL distribution")
		}
	case "ssh":
		if !validText(e.Host) || !sshHost.MatchString(e.Host) {
			return errors.New("invalid SSH host or config alias")
		}
		if e.User != "" && !sshUser.MatchString(e.User) {
			return errors.New("invalid SSH user")
		}
		if e.Port < 0 || e.Port > 65535 {
			return errors.New("invalid SSH port")
		}
		for _, p := range []string{e.IdentityFile, e.KnownHostsFile} {
			if p != "" && (!validText(p) || strings.HasPrefix(p, "-")) {
				return errors.New("invalid SSH identity or known_hosts path")
			}
		}
	default:
		return errors.New("environment type must be local, wsl, or ssh")
	}
	for _, p := range []string{e.WorkDir, e.CACertPath} {
		if p != "" && !validText(p) {
			return errors.New("invalid environment path")
		}
	}
	return nil
}

func sshArgs(e domain.Environment) []string {
	args := []string{"-T", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
	if e.Port != 0 {
		args = append(args, "-p", strconv.Itoa(e.Port))
	}
	if e.IdentityFile != "" {
		args = append(args, "-i", e.IdentityFile)
	}
	if e.KnownHostsFile != "" {
		args = append(args, "-o", "UserKnownHostsFile="+`"`+strings.ReplaceAll(e.KnownHostsFile, `"`, `\"`)+`"`)
	}
	host := e.Host
	if e.User != "" {
		host = e.User + "@" + host
	}
	return append(args, host)
}

func remoteCommand(e domain.Environment, unixArgs []string, windowsScript string) (Command, error) {
	if err := validateEnvironment(e); err != nil {
		return Command{}, err
	}
	switch e.Type {
	case "wsl":
		return Command{Path: "wsl.exe", Args: append([]string{"--distribution", e.Distribution, "--exec"}, unixArgs...)}, nil
	case "ssh":
		script := encodedPowerShell(windowsScript)
		if e.WorkerOS != "windows" {
			quoted := make([]string, len(unixArgs))
			for i, s := range unixArgs {
				quoted[i] = shellQuote(s)
			}
			script = strings.Join(quoted, " ")
		}
		return Command{Path: "ssh", Args: append(sshArgs(e), script)}, nil
	}
	return Command{}, errors.New("remote command requires WSL or SSH")
}

func BuildLaunchCommand(e domain.Environment, workerPath string) (Command, error) {
	if err := validateEnvironment(e); err != nil {
		return Command{}, err
	}
	if !validText(workerPath) {
		return Command{}, errors.New("invalid worker path")
	}
	switch e.Type {
	case "local":
		return Command{Path: workerPath, Args: []string{"--stdio"}}, nil
	case "wsl":
		if !strings.HasPrefix(workerPath, "/") || strings.Contains(workerPath, `\`) {
			return Command{}, errors.New("WSL worker path must be an absolute Linux path")
		}
		return Command{Path: "wsl.exe", Args: []string{"--distribution", e.Distribution, "--exec", workerPath, "--stdio"}}, nil
	case "ssh":
		script := "exec " + shellQuote(workerPath) + " --stdio"
		if e.WorkerOS == "windows" {
			if !isWindowsAbsolute(workerPath) || strings.ContainsAny(workerPath, `%!?&|<>^"`) {
				return Command{}, errors.New("unsupported Windows remote worker path")
			}
			script = `cmd.exe /d /s /c ""` + workerPath + `" --stdio"`
		} else if !strings.HasPrefix(workerPath, "/") || strings.Contains(workerPath, `\`) {
			return Command{}, errors.New("SSH Unix worker path must be absolute")
		}
		return Command{Path: "ssh", Args: append(sshArgs(e), script)}, nil
	}
	return Command{}, errors.New("unsupported environment")
}

func isWindowsAbsolute(s string) bool {
	return len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}
func resourcePath(dir, goos, arch string) (string, error) {
	name, err := WorkerFilename(goos, arch)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

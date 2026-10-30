package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type hostInfo struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	Home  string `json:"home"`
	Local string `json:"local"`
}

func normalizeOS(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "linux":
		return "linux"
	case "darwin":
		return "darwin"
	case "windows", "win32nt":
		return "windows"
	}
	return ""
}
func normalizeArch(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "x86_64", "amd64", "x64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}
func localWorkerRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "router-forge", "bin"), nil
	}
	return filepath.Join(home, ".local", "share", "router-forge", "bin"), nil
}
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private directory cannot be a symlink")
	}
	return securePath(dir, true)
}
func fileDigest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (m *Manager) prepareWorker(ctx context.Context, e domain.Environment) (domain.Environment, string, error) {
	if err := validateEnvironment(e); err != nil {
		return e, "", err
	}
	if e.Type == "local" {
		return m.prepareLocal(e)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	probeEnv := e
	unixProbe := []string{"sh", "-c", `uname -s && uname -m && printf '%s\n' "$HOME"`}
	windowsProbe := `$ErrorActionPreference='Stop'; @{os='windows';arch=[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString();home=[Environment]::GetFolderPath('UserProfile');local=[Environment]::GetFolderPath('LocalApplicationData')} | ConvertTo-Json -Compress`
	command, err := remoteCommand(probeEnv, unixProbe, windowsProbe)
	if err != nil {
		return e, "", err
	}
	b, _, err := runCommand(probeCtx, command, transportEnvironment(), "", nil, 16384)
	var info hostInfo
	if err == nil && probeEnv.WorkerOS == "windows" {
		err = json.Unmarshal(b, &info)
	} else if err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) != 3 {
			err = errors.New("remote OS probe returned an unsupported response")
		} else {
			info = hostInfo{OS: normalizeOS(lines[0]), Arch: normalizeArch(lines[1]), Home: strings.TrimSpace(lines[2])}
		}
	}
	if err != nil && e.Type == "ssh" && e.WorkerOS == "" {
		probeEnv.WorkerOS = "windows"
		command, _ = remoteCommand(probeEnv, nil, windowsProbe)
		b, _, err = runCommand(probeCtx, command, transportEnvironment(), "", nil, 16384)
		if err == nil {
			err = json.Unmarshal(b, &info)
		}
	}
	if err != nil {
		return e, "", errors.New("execution host OS/architecture detection failed; configure WorkerOS/WorkerArch and verify non-interactive access")
	}
	info.OS = normalizeOS(info.OS)
	info.Arch = normalizeArch(info.Arch)
	if e.WorkerOS != "" && e.WorkerOS != info.OS {
		return e, "", errors.New("configured worker OS does not match execution host")
	}
	if e.WorkerArch != "" && e.WorkerArch != info.Arch {
		return e, "", errors.New("configured worker architecture does not match execution host")
	}
	e.WorkerOS = info.OS
	e.WorkerArch = info.Arch
	name, err := WorkerFilename(info.OS, info.Arch)
	if err != nil {
		return e, "", err
	}
	source, err := resourcePath(m.workerDir, info.OS, info.Arch)
	if err != nil {
		return e, "", err
	}
	digest, err := fileDigest(source)
	if err != nil {
		return e, "", errors.New("required bundled worker resource is unavailable")
	}
	root := path.Join(info.Home, ".local", "share", "router-forge", "bin", domain.Version, digest[:16])
	if info.OS == "windows" {
		if !isWindowsAbsolute(info.Local) || !validText(info.Local) {
			return e, "", errors.New("remote local application directory is invalid")
		}
		root = strings.TrimRight(info.Local, `\`) + `\router-forge\bin\` + domain.Version + `\` + digest[:16]
	} else if !strings.HasPrefix(info.Home, "/") || !validText(info.Home) || strings.Contains(info.Home, `\`) {
		return e, "", errors.New("remote user home must be an absolute Unix path")
	}
	destination := path.Join(root, name)
	if info.OS == "windows" {
		destination = root + `\` + name
	}
	f, err := os.Open(source)
	if err != nil {
		return e, "", err
	}
	defer f.Close()
	stage := destination + "." + domain.NewID("deploy")
	script := `set -eu; umask 077; mkdir -p ` + shellQuote(root) + `; test ! -L ` + shellQuote(root) + `; cat > ` + shellQuote(stage) + `; chmod 700 ` + shellQuote(stage) + `; mv -f ` + shellQuote(stage) + ` ` + shellQuote(destination) + `; ` + shellQuote(destination) + ` --version`
	ps := `$ErrorActionPreference='Stop';$dir=` + psQuote(root) + `;$stage=` + psQuote(stage) + `;$dst=` + psQuote(destination) + `;[IO.Directory]::CreateDirectory($dir)|Out-Null;$acl=New-Object Security.AccessControl.DirectorySecurity;$acl.SetAccessRuleProtection($true,$false);$sid=[Security.Principal.WindowsIdentity]::GetCurrent().User;$rule=New-Object Security.AccessControl.FileSystemAccessRule($sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow');$acl.AddAccessRule($rule);Set-Acl -LiteralPath $dir -AclObject $acl;$f=[IO.File]::Open($stage,'CreateNew','Write');try{[Console]::OpenStandardInput().CopyTo($f)}finally{$f.Close()};Move-Item -LiteralPath $stage -Destination $dst -Force;& $dst --version`
	command, err = remoteCommand(e, []string{"sh", "-c", script}, ps)
	if err != nil {
		return e, "", err
	}
	b, _, err = runCommand(probeCtx, command, transportEnvironment(), "", f, 16384)
	if err != nil || strings.TrimSpace(string(b)) != domain.Version {
		return e, "", errors.New("private worker deployment or version verification failed")
	}
	return e, destination, nil
}
func (m *Manager) prepareLocal(e domain.Environment) (domain.Environment, string, error) {
	e.WorkerOS = runtimeOS()
	e.WorkerArch = runtimeArch()
	source, err := resourcePath(m.workerDir, e.WorkerOS, e.WorkerArch)
	if err != nil {
		return e, "", err
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return e, "", errors.New("required bundled worker resource is unavailable")
	}
	digest, err := fileDigest(source)
	if err != nil {
		return e, "", err
	}
	root := m.localRoot
	if root == "" {
		root, err = localWorkerRoot()
		if err != nil {
			return e, "", err
		}
	}
	dir := filepath.Join(root, domain.Version, digest[:16])
	if err = privateDir(dir); err != nil {
		return e, "", err
	}
	destination := filepath.Join(dir, filepath.Base(source))
	m.deployMu.Lock()
	defer m.deployMu.Unlock()
	if existing, err := fileDigest(destination); err == nil && existing == digest {
		return e, destination, nil
	}
	src, err := os.Open(source)
	if err != nil {
		return e, "", err
	}
	defer src.Close()
	stage := destination + "." + domain.NewID("deploy")
	defer os.Remove(stage)
	dst, err := os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return e, "", err
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		return e, "", err
	}
	if closeErr != nil {
		return e, "", closeErr
	}
	if err = securePath(stage, false); err != nil {
		return e, "", err
	}
	if err = os.Rename(stage, destination); err != nil {
		return e, "", err
	}
	return e, destination, nil
}

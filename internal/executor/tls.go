package executor

import (
	_ "embed"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

const containerTLSDir = "/etc/router-forge/tls"

//go:embed harbor_tls.py
var nativeHarborTLS string

var nativeHarborTLSLauncher = nativeHarborTLS + `
import runpy
import sys
apply_tls_policy(True)
launcher = sys.argv[1]
sys.argv = sys.argv[1:]
runpy.run_path(launcher, run_name="__main__")
`

const containerTLSSite = `import os
if os.environ.get("ROUTER_FORGE_SKIP_TLS_VERIFICATION") == "1":
    try:
        from router_forge_tls import apply_tls_policy
        apply_tls_policy(True)
    except Exception:
        os._exit(78)
`

func tlsProfileDir(attemptDir string) string {
	if strings.HasPrefix(attemptDir, "/") {
		return path.Join(attemptDir, "tls")
	}
	return filepath.Join(attemptDir, "tls")
}

func tlsMountSource(jobsDir string) string {
	if strings.HasPrefix(jobsDir, "/") {
		return tlsProfileDir(path.Dir(jobsDir))
	}
	return tlsProfileDir(filepath.Dir(jobsDir))
}

func writeTLSProfile(attemptDir string) error {
	directory := tlsProfileDir(attemptDir)
	if err := privateDir(directory); err != nil {
		return errors.New("could not create private per-attempt TLS client configuration")
	}
	python := filepath.Join(directory, "python")
	if err := privateDir(python); err != nil {
		return errors.New("could not create private Python TLS client configuration")
	}
	for name, content := range map[string]string{
		".curlrc":                    "insecure\nproxy-insecure\n",
		"wgetrc":                     "check_certificate = off\n",
		"python/sitecustomize.py":    containerTLSSite,
		"python/router_forge_tls.py": nativeHarborTLS,
	} {
		filename := filepath.Join(directory, filepath.FromSlash(name))
		if err := writePrivate(filename, []byte(content)); err != nil {
			return errors.New("could not persist private TLS client configuration")
		}
		if err := os.Chmod(filename, 0644); err != nil {
			return errors.New("could not make credential-free TLS configuration readable by container clients")
		}
	}
	for _, name := range []string{directory, python} {
		if err := os.Chmod(name, 0755); err != nil {
			return errors.New("could not make credential-free TLS directories accessible to container clients")
		}
	}
	return nil
}

var nativeHarborNetworkLauncher = nativeHarborTLS + "\n" + nativeHarborNetwork + `
import sys
try:
    run_network_harbor(sys.argv[1], sys.argv[2], sys.argv[3])
except (UnsupportedNetworkPolicy, UnsupportedTLSPolicy):
    sys.exit(78)
`

func harborNetworkRunCommand(e domain.Environment, harbor, configPath, networkPath string) (Command, error) {
	python, err := nativeHarborPython(e, harbor)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: python, Args: []string{"-I", "-B", "-c", nativeHarborNetworkLauncher, harbor, configPath, networkPath}}, nil
}

func harborRunCommand(e domain.Environment, harbor, configPath string) (Command, error) {
	if !e.SkipTLSVerification {
		return Command{Path: harbor, Args: []string{"run", "--config", configPath}}, nil
	}
	python, err := nativeHarborPython(e, harbor)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: python, Args: []string{"-I", "-c", nativeHarborTLSLauncher, harbor, "run", "--config", configPath}}, nil
}

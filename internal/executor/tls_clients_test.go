package executor

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func runTLSClientFixture(ctx context.Context, command Command, env []string) ([]byte, error) {
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = env
	output := &boundedBuffer{limit: 8192}
	cmd.Stdout, cmd.Stderr = output, output
	process, err := startOwned(ctx, cmd)
	if err != nil {
		return nil, err
	}
	err = process.wait()
	data, truncated := output.bytes()
	if truncated {
		return nil, fmt.Errorf("TLS fixture output exceeded its limit")
	}
	return data, err
}

func newUntrustedTLSFixture(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	// A fresh key prevents the fixture from matching an already trusted certificate.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned-tls-fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	return server
}

func TestNativeTLSClientsRequireExplicitOptInAndRestoreStrict(t *testing.T) {
	var wheel bytes.Buffer
	archive := zip.NewWriter(&wheel)
	for name, data := range map[string]string{
		"router_forge_tls_fixture-1.0.0.dist-info/METADATA": "Metadata-Version: 2.1\nName: router-forge-tls-fixture\nVersion: 1.0.0\nRequires-Python: >=3.9\n\n",
		"router_forge_tls_fixture-1.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\nGenerator: router-forge-fixture\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
		"router_forge_tls_fixture-1.0.0.dist-info/RECORD":   "",
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	server := newUntrustedTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public":
			io.WriteString(w, "router-forge-tls-fixture")
		case "/git/info/refs":
			io.WriteString(w, strings.Repeat("1", 40)+"\trefs/heads/main\n")
		case "/git/HEAD":
			io.WriteString(w, "ref: refs/heads/main\n")
		case "/simple/router-forge-tls-fixture/":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<a href="/router_forge_tls_fixture-1.0.0-py3-none-any.whl">router_forge_tls_fixture-1.0.0-py3-none-any.whl</a>`)
		case "/router_forge_tls_fixture-1.0.0-py3-none-any.whl":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(wheel.Bytes())
		case "/npm/router-forge-tls-fixture":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"name":"router-forge-tls-fixture","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"router-forge-tls-fixture","version":"1.0.0"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	type client struct {
		name, marker string
		command      func(root string, skip bool) Command
	}
	var clients []client
	if python, err := exec.LookPath("python"); err == nil {
		for _, name := range []string{"stdlib", "httpx", "requests"} {
			if name != "stdlib" {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, _, err := runCommand(ctx, Command{Path: python, Args: []string{"-I", "-c", "import " + name}}, transportEnvironment(), "", nil, 1024)
				cancel()
				if err != nil {
					t.Logf("optional Python %s is not installed", name)
					continue
				}
			}
			script := "import sys\napply_tls_policy(sys.argv[2] == 'true')\n"
			switch name {
			case "stdlib":
				script += "import urllib.request\nprint(urllib.request.urlopen(sys.argv[1], timeout=3).read().decode())\n"
			case "httpx":
				script += "import asyncio, httpx\nprint(httpx.Client(timeout=3).get(sys.argv[1]).text)\nasync def fetch():\n    async with httpx.AsyncClient(timeout=3) as client:\n        print((await client.get(sys.argv[1])).text)\nasyncio.run(fetch())\n"
			case "requests":
				script += "import requests\nprint(requests.get(sys.argv[1], timeout=3).text)\n"
			}
			clients = append(clients, client{name: "host-python-" + name, marker: "router-forge-tls-fixture", command: func(_ string, skip bool) Command {
				return Command{Path: python, Args: []string{"-I", "-c", nativeHarborTLS + "\n" + script, server.URL + "/public", fmt.Sprint(skip)}}
			}})
		}
		clients = append(clients, client{name: "container-python-sitecustomize", marker: "router-forge-tls-fixture", command: func(_ string, _ bool) Command {
			return Command{Path: python, Args: []string{"-c", "import urllib.request, sys; print(urllib.request.urlopen(sys.argv[1], timeout=3).read().decode())", server.URL + "/public"}}
		}})
	}
	curl, curlErr := exec.LookPath("curl")
	if runtime.GOOS == "linux" {
		curl, curlErr = "/usr/bin/curl", nil
		if _, err := os.Stat(curl); err != nil {
			curlErr = err
		}
	}
	if curlErr == nil {
		clients = append(clients, client{name: "curl", marker: "router-forge-tls-fixture", command: func(_ string, _ bool) Command {
			return Command{Path: curl, Args: []string{"--silent", "--show-error", "--fail", "--max-time", "4", server.URL + "/public"}}
		}})
	}
	if wget, err := exec.LookPath("wget"); err == nil {
		clients = append(clients, client{name: "wget", marker: "router-forge-tls-fixture", command: func(_ string, _ bool) Command {
			return Command{Path: wget, Args: []string{"--no-verbose", "--tries=1", "--timeout=4", "-O", "-", server.URL + "/public"}}
		}})
	}
	if git, err := exec.LookPath("git"); err == nil {
		clients = append(clients, client{name: "git", marker: strings.Repeat("1", 40), command: func(_ string, _ bool) Command {
			return Command{Path: git, Args: []string{"ls-remote", server.URL + "/git"}}
		}})
	}
	if node, err := exec.LookPath("node"); err == nil {
		clients = append(clients, client{name: "node", marker: "router-forge-tls-fixture", command: func(_ string, _ bool) Command {
			return Command{Path: node, Args: []string{"-e", `require('node:https').get(process.argv[1], r => {r.pipe(process.stdout)}).on('error', e => {console.error(e.code); process.exitCode = 1})`, server.URL + "/public"}}
		}})
		if npm, err := exec.LookPath("npm"); err == nil {
			npmJS, _ := filepath.EvalSymlinks(npm)
			if !strings.HasSuffix(npmJS, ".js") {
				npmJS = filepath.Join(filepath.Dir(npm), "node_modules", "npm", "bin", "npm-cli.js")
			}
			if info, err := os.Stat(npmJS); err == nil && info.Mode().IsRegular() {
				clients = append(clients, client{name: "npm", marker: "1.0.0", command: func(root string, _ bool) Command {
					return Command{Path: node, Args: []string{npmJS, "view", "router-forge-tls-fixture", "version", "--json", "--registry", server.URL + "/npm", "--fetch-retries=0", "--fetch-timeout=4000", "--cache", filepath.Join(root, "npm-cache")}}
				}})
			}
		}
	}
	if uv, err := exec.LookPath("uv"); err == nil {
		clients = append(clients, client{name: "uv", marker: "router-forge-tls-fixture==1.0.0", command: func(root string, _ bool) Command {
			return Command{Path: uv, Args: []string{"--no-cache", "--no-config", "pip", "compile", "--python-version", "3.12", "--no-python-downloads", "--index-url", server.URL + "/simple", filepath.Join(root, "requirements.in")}}
		}})
	}
	if len(clients) == 0 {
		t.Skip("no installed runtime client is available")
	}
	for _, c := range clients {
		t.Run(c.name, func(t *testing.T) {
			for _, mode := range []struct {
				name string
				skip bool
			}{{"strict-default", false}, {"explicit-skip", true}, {"strict-restored", false}} {
				t.Run(mode.name, func(t *testing.T) {
					root := projectTestDir(t)
					for _, name := range []string{"home", "scratch"} {
						if err := privateDir(filepath.Join(root, name)); err != nil {
							t.Fatal(err)
						}
					}
					if mode.skip {
						if err := writeTLSProfile(root); err != nil {
							t.Fatal(err)
						}
					}
					for _, name := range []string{"git-config", "npm-user-config", "npm-global-config"} {
						if err := writePrivate(filepath.Join(root, name), nil); err != nil {
							t.Fatal(err)
						}
					}
					if err := writePrivate(filepath.Join(root, "requirements.in"), []byte("router-forge-tls-fixture==1.0.0\n")); err != nil {
						t.Fatal(err)
					}
					env := nativeProcessEnv(domain.Environment{SkipTLSVerification: mode.skip}, root, "")
					env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, "git-config"), "npm_config_userconfig="+filepath.Join(root, "npm-user-config"), "npm_config_globalconfig="+filepath.Join(root, "npm-global-config"))
					env = append(env, "UV_HTTP_RETRIES=0", "UV_HTTP_TIMEOUT=4")
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					output, err := runTLSClientFixture(ctx, c.command(root, mode.skip), env)
					if mode.skip {
						if err != nil || !strings.Contains(string(output), c.marker) {
							t.Fatalf("explicit scoped TLS opt-in was not honored: %v\n%s", err, output)
						}
					} else if err == nil || !strings.Contains(strings.ToLower(string(output)), "cert") && !strings.Contains(string(output), "SEC_E_UNTRUSTED_ROOT") {
						t.Fatalf("strict TLS did not reject the untrusted certificate: %v\n%s", err, output)
					}
				})
			}
		})
	}
}

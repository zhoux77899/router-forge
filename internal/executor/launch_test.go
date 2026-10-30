package executor

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestLaunchCommandsKeepArgumentsSeparateAndQuoteRemotePaths(t *testing.T) {
	local, err := BuildLaunchCommand(domain.Environment{Type: "local"}, `D:\Workers\router forge.exe`)
	if err != nil || local.Path != `D:\Workers\router forge.exe` || !reflect.DeepEqual(local.Args, []string{"--stdio"}) {
		t.Fatalf("local launch: %#v, %v", local, err)
	}
	ssh, err := BuildLaunchCommand(domain.Environment{Type: "ssh", Host: "bench.example", User: "runner", Port: 2222, IdentityFile: `D:\Keys\runner key`, KnownHostsFile: `D:\Keys\known hosts`}, "/home/runner/O'Brien/bin/worker")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-T", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-p", "2222", "-i", `D:\Keys\runner key`, "-o", `UserKnownHostsFile="D:\Keys\known hosts"`, "runner@bench.example", "exec '/home/runner/O'\"'\"'Brien/bin/worker' --stdio"}
	if ssh.Path != "ssh" || !reflect.DeepEqual(ssh.Args, want) {
		t.Fatalf("SSH command = %#v", ssh)
	}
	if runtime.GOOS == "windows" {
		wsl, err := BuildLaunchCommand(domain.Environment{Type: "wsl", Distribution: "Ubuntu"}, "/home/runner/worker")
		if err != nil || !reflect.DeepEqual(wsl.Args, []string{"--distribution", "Ubuntu", "--exec", "/home/runner/worker", "--stdio"}) {
			t.Fatalf("WSL command: %#v %v", wsl, err)
		}
	}
}

func TestLaunchRejectsOptionsAndShellControlInEnvironment(t *testing.T) {
	for _, e := range []domain.Environment{
		{Type: "ssh", Host: "-oProxyCommand=evil"},
		{Type: "ssh", Host: "bench; touch bad"},
		{Type: "ssh", Host: "bench.example", User: "root\n"},
		{Type: "ssh", Host: "bench.example", Port: 65536},
		{Type: "wsl", Distribution: "--exec"},
	} {
		if _, err := BuildLaunchCommand(e, "/home/runner/worker"); err == nil {
			t.Fatalf("accepted unsafe environment: %#v", e)
		}
	}
	if _, err := BuildLaunchCommand(domain.Environment{Type: "ssh", Host: "bench.example"}, "/home/runner/worker\nexit"); err == nil {
		t.Fatal("accepted control character in executable path")
	}
	if _, err := WorkerFilename("linux", "x86_64"); err == nil {
		t.Fatal("unnormalized architecture accepted")
	}
	filename, err := WorkerFilename("windows", "amd64")
	if err != nil || !strings.HasSuffix(filename, "-windows-amd64.exe") {
		t.Fatalf("filename = %q %v", filename, err)
	}
}

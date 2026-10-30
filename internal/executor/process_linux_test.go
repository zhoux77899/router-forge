package executor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnedProcessKillsIgnoringDescendantAfterLeaderExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	script := `sh -c 'trap "" TERM; while :; do sleep 1; done' >/dev/null 2>&1 &
 child=$!
 printf '%s\n' "$child"
 trap 'exit 0' TERM
 wait "$child"`
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdout = write
	proc, err := startOwned(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { proc.stop(); _ = proc.wait() }()
	line, err := bufio.NewReader(read).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	group, err := syscall.Getpgid(pid)
	if err != nil || group != proc.cmd.Process.Pid {
		t.Fatal("test child is not in its explicitly owned process group")
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		t.Fatal("owned leader did not stop")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			end := strings.LastIndex(string(data), ") ")
			if end >= 0 && strings.HasPrefix(string(data)[end+2:], "Z ") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	// On a failing implementation, remove only the still-verified test child.
	if group, err := syscall.Getpgid(pid); err == nil && group == proc.cmd.Process.Pid {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	t.Fatal("ignoring descendant survived cancellation after its leader exited")
}

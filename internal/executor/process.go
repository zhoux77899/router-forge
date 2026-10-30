package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type processControl interface {
	terminate() error
	close() error
}
type ownedProcess struct {
	cmd      *exec.Cmd
	control  processControl
	done     chan struct{}
	mu       sync.Mutex
	err      error
	stopOnce sync.Once
}

func startOwned(ctx context.Context, cmd *exec.Cmd) (*ownedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	control, err := startPlatformProcess(cmd)
	if err != nil {
		return nil, err
	}
	p := &ownedProcess{cmd: cmd, control: control, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.err = err
		close(p.done)
		p.mu.Unlock()
		_ = control.close()
	}()
	go func() {
		select {
		case <-ctx.Done():
			p.stop()
		case <-p.done:
		}
	}()
	return p, nil
}
func (p *ownedProcess) stop() {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = p.control.terminate()
	})
}
func (p *ownedProcess) wait() error { <-p.done; p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *ownedProcess) exitCode() int {
	<-p.done
	if p.cmd.ProcessState == nil {
		return -1
	}
	return p.cmd.ProcessState.ExitCode()
}

type boundedBuffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) > b.limit-b.b.Len() {
		p = p[:max(0, b.limit-b.b.Len())]
		b.truncated = true
	}
	_, _ = b.b.Write(p)
	return n, nil
}
func (b *boundedBuffer) bytes() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.b.Bytes()...), b.truncated
}

func transportEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "HOME": true, "USER": true, "LOGNAME": true, "USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true, "LOCALAPPDATA": true, "APPDATA": true, "SSH_AUTH_SOCK": true, "LANG": true, "LC_ALL": true, "TMPDIR": true, "TEMP": true, "TMP": true}
	var out []string
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if ok && allowed[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}

func runCommand(ctx context.Context, c Command, env []string, dir string, stdin io.Reader, limit int) ([]byte, int, error) {
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = stdin
	out := &boundedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	proc, err := startOwned(ctx, cmd)
	if err != nil {
		return nil, -1, errors.New("could not start " + commandLabel(c.Path))
	}
	err = proc.wait()
	data, truncated := out.bytes()
	if ctx.Err() != nil {
		return data, proc.exitCode(), ctx.Err()
	}
	if truncated {
		return nil, proc.exitCode(), errors.New("command output exceeded limit")
	}
	if err != nil {
		return data, proc.exitCode(), errors.New(commandLabel(c.Path) + " exited unsuccessfully")
	}
	return data, proc.exitCode(), nil
}
func commandLabel(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	i := strings.LastIndex(path, "/")
	if i >= 0 {
		return path[i+1:]
	}
	return path
}

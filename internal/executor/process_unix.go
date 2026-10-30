//go:build !windows

package executor

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type unixControl struct {
	mu       sync.Mutex
	pid      int
	closed   bool
	stopping bool
}

func startPlatformProcess(cmd *exec.Cmd) (processControl, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &unixControl{pid: cmd.Process.Pid}, nil
}
func (c *unixControl) terminate() error {
	c.mu.Lock()
	if c.closed || c.stopping {
		c.mu.Unlock()
		return nil
	}
	c.stopping = true
	err := syscall.Kill(-c.pid, syscall.SIGTERM)
	c.mu.Unlock()
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		c.mu.Lock()
		defer c.mu.Unlock()
		// The group can still contain children after its leader has exited.
		_ = syscall.Kill(-c.pid, syscall.SIGKILL)
	}()
	return err
}
func (c *unixControl) close() error {
	err := c.terminate()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return err
}
func securePath(path string, isDir bool) error {
	mode := os.FileMode(0600)
	if isDir {
		mode = 0700
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("filesystem cannot enforce private user-owned permissions; use the native Linux home work directory, not unsafe WSL DrvFS")
	}
	return nil
}

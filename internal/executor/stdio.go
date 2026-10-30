package executor

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type pipeAddress string

func (a pipeAddress) Network() string { return "stdio" }
func (a pipeAddress) String() string  { return string(a) }

type stdioConn struct {
	input      io.ReadCloser
	output     io.WriteCloser
	once       sync.Once
	writeMu    sync.Mutex
	deadlineMu sync.Mutex
	deadline   *time.Timer
}

func newStdioConn(in io.ReadCloser, out io.WriteCloser) *stdioConn {
	return &stdioConn{input: in, output: out}
}
func (c *stdioConn) Read(b []byte) (int, error) { return c.input.Read(b) }
func (c *stdioConn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.output.Write(b)
}
func (c *stdioConn) Close() error {
	var err error
	c.once.Do(func() {
		c.deadlineMu.Lock()
		if c.deadline != nil {
			c.deadline.Stop()
		}
		c.deadlineMu.Unlock()
		err = errors.Join(c.input.Close(), c.output.Close())
	})
	return err
}
func (c *stdioConn) LocalAddr() net.Addr  { return pipeAddress("parent") }
func (c *stdioConn) RemoteAddr() net.Addr { return pipeAddress("worker") }
func (c *stdioConn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	if c.deadline != nil {
		c.deadline.Stop()
		c.deadline = nil
	}
	if !t.IsZero() {
		c.deadline = time.AfterFunc(time.Until(t), func() { _ = c.Close() })
	}
	return nil
}
func (c *stdioConn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *stdioConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

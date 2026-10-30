package netpolicy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxRelayStreams     = 64
	maxRelayConnections = 256
	copyBufferBytes     = 32 << 10
)

// ProxyRelay owns its listener and all HTTP/tunnel connections. URL is an
// HTTP proxy address without userinfo; it is safe for downstream configuration.
// Close is idempotent and waits for owned forwarding streams to finish.
type ProxyRelay struct {
	URL string

	ctx       context.Context
	cancel    context.CancelFunc
	listener  net.Listener
	server    *http.Server
	transport *Transport
	dial      func(context.Context, string, string) (net.Conn, error)
	authorize func(net.IP) bool
	streams   chan struct{}
	workers   sync.WaitGroup
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	closing   bool
	conns     map[*relayConn]struct{}
}

// StartProxyRelay starts a relay on an ephemeral port at an explicitly supplied
// loopback/private IP. authorize must verify each request's actual peer IP; the
// caller, not the relay, decides which exact workload addresses are permitted.
func StartProxyRelay(ctx context.Context, bindIP net.IP, policy Policy, authorize func(remoteIP net.IP) bool) (*ProxyRelay, error) {
	if ctx == nil || authorize == nil {
		return nil, errors.New("network policy: relay requires a context and remote-IP authorization")
	}
	if bindIP.To16() == nil || bindIP.IsUnspecified() || bindIP.IsMulticast() ||
		(!bindIP.IsLoopback() && !bindIP.IsPrivate()) {
		return nil, errors.New("network policy: relay bind must be an explicit loopback or private unicast IP")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, err := compilePolicy(policy)
	if err != nil {
		return nil, err
	}
	if policy.ProxyMode != "proxy" {
		return nil, errors.New("network policy: a relay requires an explicitly configured upstream proxy")
	}

	listenConfig := net.ListenConfig{KeepAlive: -1}
	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(bindIP.String(), "0"))
	if err != nil {
		return nil, errors.New("network policy: cannot start proxy relay listener")
	}
	relayCtx, cancel := context.WithCancel(ctx)
	relay := &ProxyRelay{
		URL:       (&url.URL{Scheme: "http", Host: listener.Addr().String()}).String(),
		ctx:       relayCtx,
		cancel:    cancel,
		authorize: authorize,
		streams:   make(chan struct{}, maxRelayStreams),
		closed:    make(chan struct{}),
		conns:     make(map[*relayConn]struct{}),
		transport: newTransport(config),
		dial: (&net.Dialer{
			Timeout: dialTimeout, KeepAlive: -1, FallbackDelay: -1,
		}).DialContext,
	}
	relay.listener = &relayListener{Listener: listener, relay: relay}
	relay.transport.base.DialContext = relay.dialContext
	relay.server = &http.Server{
		Handler:           http.HandlerFunc(relay.serveHTTP),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       operationTimeout,
		WriteTimeout:      operationTimeout,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          log.New(io.Discard, "", 0),
		BaseContext:       func(net.Listener) context.Context { return relayCtx },
	}
	go func() {
		_ = relay.server.Serve(relay.listener)
		_ = relay.Close()
	}()
	go func() {
		<-relayCtx.Done()
		_ = relay.Close()
	}()
	return relay, nil
}

// Done closes after the listener and active connections have been closed and
// owned forwarding streams have exited.
func (relay *ProxyRelay) Done() <-chan struct{} { return relay.closed }
func (relay *ProxyRelay) String() string        { return relay.URL }
func (relay *ProxyRelay) GoString() string      { return relay.URL }

func (relay *ProxyRelay) Close() error {
	relay.closeOnce.Do(func() {
		relay.mu.Lock()
		relay.closing = true
		connections := make([]*relayConn, 0, len(relay.conns))
		for conn := range relay.conns {
			connections = append(connections, conn)
		}
		relay.mu.Unlock()
		relay.cancel()
		_ = relay.listener.Close()
		_ = relay.server.Close()
		for _, conn := range connections {
			_ = conn.Close()
		}
		relay.transport.CloseIdleConnections()
		relay.workers.Wait()
		close(relay.closed)
	})
	return nil
}

func (relay *ProxyRelay) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	remoteIP := net.ParseIP(host)
	if err != nil || remoteIP == nil || !relay.authorize(remoteIP) {
		http.Error(writer, "proxy relay access denied", http.StatusForbidden)
		return
	}
	if !relay.acquireStream() {
		http.Error(writer, "proxy relay capacity unavailable", http.StatusServiceUnavailable)
		return
	}
	defer relay.releaseStream()
	ctx, cancel := context.WithTimeout(request.Context(), operationTimeout)
	defer cancel()
	if request.Method == http.MethodConnect {
		relay.connect(ctx, writer, request)
		return
	}
	if !request.URL.IsAbs() || request.URL.Scheme != "http" || request.URL.Hostname() == "" || request.URL.User != nil {
		http.Error(writer, "proxy relay requires an absolute HTTP URL or CONNECT", http.StatusBadRequest)
		return
	}

	outgoing := request.Clone(ctx)
	outgoing.RequestURI = ""
	outgoing.Host = outgoing.URL.Host
	outgoing.Trailer = nil
	outgoing.TransferEncoding = nil
	stripRelayHeaders(outgoing.Header)
	response, err := relay.forwardHTTP(ctx, outgoing)
	if err != nil {
		http.Error(writer, "proxy relay upstream request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	stripRelayHeaders(response.Header)
	for name, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.CopyBuffer(flushWriter{writer}, response.Body, make([]byte, copyBufferBytes))
}

func (relay *ProxyRelay) acquireStream() bool {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if relay.closing {
		return false
	}
	select {
	case relay.streams <- struct{}{}:
		relay.workers.Add(1)
		return true
	default:
		return false
	}
}

func (relay *ProxyRelay) releaseStream() {
	<-relay.streams
	relay.workers.Done()
}

func stripRelayHeaders(header http.Header) {
	for _, key := range []string{"Connection", "Proxy-Connection"} {
		for _, value := range header.Values(key) {
			for _, name := range strings.Split(value, ",") {
				header.Del(strings.TrimSpace(name))
			}
		}
	}
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade", "Cookie", "Cookie2", "Set-Cookie",
	} {
		header.Del(name)
	}
}

type flushWriter struct{ http.ResponseWriter }

func (writer flushWriter) Write(data []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(data)
	if err == nil {
		err = http.NewResponseController(writer.ResponseWriter).Flush()
	}
	return n, err
}

func (relay *ProxyRelay) connect(ctx context.Context, writer http.ResponseWriter, request *http.Request) {
	target, targetURL, err := connectTarget(request.RequestURI)
	if err != nil {
		http.Error(writer, "proxy relay requires a CONNECT host and numeric port", http.StatusBadRequest)
		return
	}
	upstream, upstreamReader, err := relay.openTunnel(ctx, target, targetURL)
	if err != nil {
		http.Error(writer, "proxy relay upstream CONNECT failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	downstream, buffered, err := http.NewResponseController(writer).Hijack()
	if err != nil {
		return
	}
	defer downstream.Close()
	deadline, _ := ctx.Deadline()
	_ = upstream.SetDeadline(deadline)
	_ = downstream.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = upstream.Close()
		_ = downstream.Close()
	})
	defer stop()
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}

	results := make(chan error, 2)
	go func() {
		_, err := io.CopyBuffer(upstream, buffered.Reader, make([]byte, copyBufferBytes))
		closeWrite(upstream)
		results <- err
	}()
	go func() {
		_, err := io.CopyBuffer(downstream, upstreamReader, make([]byte, copyBufferBytes))
		closeWrite(downstream)
		results <- err
	}()
	if err := <-results; err != nil {
		_ = upstream.Close()
		_ = downstream.Close()
	}
	<-results
}

func connectTarget(authority string) (string, *url.URL, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || host == "" || strings.ContainsAny(host, "\r\n\t /?#@") {
		return "", nil, errors.New("network policy: invalid CONNECT authority")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", nil, errors.New("network policy: invalid CONNECT port")
	}
	target := net.JoinHostPort(host, strconv.Itoa(number))
	return target, &url.URL{Scheme: "https", Host: target}, nil
}

func (relay *ProxyRelay) openTunnel(ctx context.Context, target string, targetURL *url.URL) (net.Conn, io.Reader, error) {
	proxy, err := relay.transport.config.proxyForURL(targetURL)
	if err != nil {
		return nil, nil, errRequestFailed
	}
	if proxy == nil {
		conn, err := relay.dialContext(ctx, "tcp", target)
		return conn, conn, err
	}
	conn, err := relay.dialContext(ctx, "tcp", proxyAddress(proxy))
	if err != nil {
		return nil, nil, errRequestFailed
	}
	ready := false
	defer func() {
		if !ready {
			_ = conn.Close()
		}
	}()
	deadline := time.Now().Add(headerTimeout)
	if operationDeadline, ok := ctx.Deadline(); ok && operationDeadline.Before(deadline) {
		deadline = operationDeadline
	}
	_ = conn.SetDeadline(deadline)
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if proxy.Scheme == "https" {
		tlsConfig := relay.transport.config.tlsConfig.Clone()
		tlsConfig.ServerName = proxy.Hostname()
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, nil, errRequestFailed
		}
		conn = tlsConn
	}
	header := make(http.Header)
	if relay.transport.config.authorization != "" {
		header.Set("Proxy-Authorization", relay.transport.config.authorization)
	}
	connectRequest := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: header,
	}
	if err := connectRequest.Write(conn); err != nil {
		return nil, nil, errRequestFailed
	}
	limited := &headerReader{reader: conn, remaining: maxHeaderBytes, limited: true}
	buffered := bufio.NewReader(limited)
	response, err := http.ReadResponse(buffered, connectRequest)
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, nil, errRequestFailed
	}
	// Preserve read-ahead tunnel bytes, but stop limiting once headers are parsed.
	limited.limited = false
	ready = true
	return conn, buffered, nil
}

type headerReader struct {
	reader    io.Reader
	remaining int
	limited   bool
}

func (reader *headerReader) Read(data []byte) (int, error) {
	if reader.limited {
		if reader.remaining == 0 {
			return 0, errors.New("network policy: response headers exceed limit")
		}
		if len(data) > reader.remaining {
			data = data[:reader.remaining]
		}
	}
	n, err := reader.reader.Read(data)
	if reader.limited {
		reader.remaining -= n
	}
	return n, err
}

func (relay *ProxyRelay) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := relay.dial(ctx, network, address)
	if err != nil {
		return nil, errRequestFailed
	}
	owned, err := relay.own(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return owned, nil
}

func (relay *ProxyRelay) own(conn net.Conn) (*relayConn, error) {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if relay.closing || len(relay.conns) >= maxRelayConnections {
		return nil, errors.New("network policy: relay connection capacity unavailable")
	}
	owned := &relayConn{Conn: conn, relay: relay}
	relay.conns[owned] = struct{}{}
	return owned, nil
}

type relayListener struct {
	net.Listener
	relay *ProxyRelay
}

func (listener *relayListener) Accept() (net.Conn, error) {
	for {
		conn, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		owned, err := listener.relay.own(conn)
		if err == nil {
			return owned, nil
		}
		_ = conn.Close()
	}
}

type relayConn struct {
	net.Conn
	relay *ProxyRelay
	once  sync.Once
	err   error
}

func (conn *relayConn) Close() error {
	conn.once.Do(func() {
		conn.err = conn.Conn.Close()
		conn.relay.mu.Lock()
		delete(conn.relay.conns, conn)
		conn.relay.mu.Unlock()
	})
	return conn.err
}

func (conn *relayConn) CloseWrite() error {
	if writer, ok := conn.Conn.(interface{ CloseWrite() error }); ok {
		return writer.CloseWrite()
	}
	return nil
}

func closeWrite(conn net.Conn) {
	if writer, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
}

package executor

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type modelRelay struct {
	client        *http.Client
	token         string
	protocol      domain.Protocol
	streams       chan struct{}
	ready         <-chan struct{}
	probeToken    string
	beforeForward func()
}

func NewModelRelay(client *http.Client, token string, protocol domain.Protocol) http.Handler {
	return &modelRelay{client: client, token: token, protocol: protocol, streams: make(chan struct{}, maxModelStreams)}
}
func protocolRouteAllowed(protocol domain.Protocol, method, path string) bool {
	if !modelRouteAllowed(method, path) {
		return false
	}
	switch protocol {
	case domain.ChatCompletions:
		return path == "/v1/chat/completions"
	case domain.AnthropicMessages:
		return path == "/v1/messages"
	case domain.Responses:
		return path == "/v1/responses" || strings.HasPrefix(path, "/v1/responses/")
	}
	return false
}
func leaseHeader(r *http.Request) (string, bool) {
	bearer := ""
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		if !strings.HasPrefix(authorization, "Bearer ") {
			return "", false
		}
		bearer = strings.TrimPrefix(authorization, "Bearer ")
	}
	key := r.Header.Get("x-api-key")
	if bearer != "" && key != "" && subtle.ConstantTimeCompare([]byte(bearer), []byte(key)) != 1 {
		return "", false
	}
	if bearer != "" {
		return bearer, true
	}
	return key, key != ""
}
func (r *modelRelay) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if r.client == nil || !protocolRouteAllowed(r.protocol, request.Method, request.URL.Path) || request.URL.RawQuery != "" {
		http.NotFound(w, request)
		return
	}
	select {
	case r.streams <- struct{}{}:
		defer func() { <-r.streams }()
	default:
		http.Error(w, "relay concurrency limit reached", 429)
		return
	}
	if request.ContentLength > maxModelBody {
		http.Error(w, "request body too large", 413)
		return
	}
	token, valid := leaseHeader(request)
	if r.probeToken != "" && valid && subtle.ConstantTimeCompare([]byte(token), []byte(r.probeToken)) == 1 && request.Header.Get("X-Router-Forge-Probe") == r.probeToken {
		data, err := io.ReadAll(io.LimitReader(request.Body, 1025))
		if err != nil || len(data) > 1024 {
			http.Error(w, "invalid mock reachability probe", 400)
			return
		}
		w.Header().Set("X-Router-Forge-Probe", r.probeToken)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !valid || r.token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
		http.Error(w, "invalid attempt lease", 401)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxModelBody+1))
	if err != nil || len(body) > maxModelBody {
		http.Error(w, "request body too large", 413)
		return
	}
	if r.ready != nil {
		if r.beforeForward != nil {
			r.beforeForward()
		}
		select {
		case <-r.ready:
		case <-request.Context().Done():
			return
		}
	}
	upstream := request.Clone(request.Context())
	upstream.URL = &url.URL{Scheme: "http", Host: "router-forge", Path: request.URL.Path}
	upstream.Host = "router-forge"
	upstream.RequestURI = ""
	upstream.Body = io.NopCloser(bytes.NewReader(body))
	upstream.ContentLength = int64(len(body))
	upstream.GetBody = nil
	upstream.Header = request.Header.Clone()
	stripHopHeaders(upstream.Header)
	upstream.Header.Del("X-Router-Forge-Probe")
	upstream.Header.Del("Cookie")
	response, err := r.client.Do(upstream)
	if err != nil {
		if request.Context().Err() == nil {
			http.Error(w, "private gateway connection failed", 502)
		}
		return
	}
	defer response.Body.Close()
	stripHopHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.CopyBuffer(flushWriter{ResponseWriter: w}, response.Body, make([]byte, 32<<10))
}
func stripHopHeaders(header http.Header) {
	for _, v := range header.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

type flushWriter struct{ http.ResponseWriter }

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

type runningRelay struct {
	server     *http.Server
	listener   net.Listener
	URL        string
	ProbeToken string
}

func startRelay(ctx context.Context, client *http.Client, bindIP, token string, protocol domain.Protocol, ready <-chan struct{}, beforeForward ...func()) (*runningRelay, error) {
	if err := validateBindIP(bindIP); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(bindIP, "0"))
	if err != nil {
		return nil, errors.New("could not bind worker-owned Docker relay address")
	}
	handler := NewModelRelay(client, token, protocol).(*modelRelay)
	handler.ready = ready
	if len(beforeForward) == 1 {
		handler.beforeForward = beforeForward[0]
	}
	handler.probeToken = domain.NewID("probe")
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	relay := &runningRelay{server: server, listener: listener, URL: "http://router-forge-gateway:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), ProbeToken: handler.probeToken}
	go func() { _ = server.Serve(listener) }()
	return relay, nil
}
func (r *runningRelay) close() { _ = r.server.Close(); _ = r.listener.Close() }
func relayBaseURL(root string, protocol domain.Protocol) string {
	if protocol == domain.AnthropicMessages {
		return root
	}
	return strings.TrimRight(root, "/") + "/v1"
}

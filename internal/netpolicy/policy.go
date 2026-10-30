package netpolicy

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
)

const (
	operationTimeout    = 10 * time.Minute
	dialTimeout         = 15 * time.Second
	headerTimeout       = 30 * time.Second
	tlsHandshakeTimeout = 15 * time.Second
	maxHeaderBytes      = 32 << 10
	maxCABytes          = 4 << 20
	maxAuthBytes        = 8 << 10
	maxClientStreams    = 64
)

var (
	errRequestFailed = errors.New("network policy: HTTP request failed")
	errProxyRejected = errors.New("network policy: upstream proxy rejected authentication")
)

// Policy contains only explicit network choices. It is copied and compiled when
// a transport or relay is created; changing it afterwards has no effect.
type Policy struct {
	ProxyMode       string
	ProxyURL        string
	ProxyTargets    []string
	NoProxy         string
	TLSMode         string
	TrustMode       string
	CACertPath      string
	TLSRiskAccepted bool
	Auth            *Credentials `json:"-"`
}

// Credentials are used only on the connection to the configured upstream proxy.
// Formatting and JSON omit their values; downstream configuration must use the
// relay URL instead. Persisted credentials belong to the caller's private store.
type Credentials struct {
	Username string `json:"-"`
	Password string `json:"-"`
}

func (Credentials) String() string   { return "<redacted proxy credentials>" }
func (Credentials) GoString() string { return "<redacted proxy credentials>" }

type policyConfig struct {
	proxyForURL   func(*url.URL) (*url.URL, error)
	tlsConfig     *tls.Config
	authorization string
	caCertSHA256  string
}

// Transport is an HTTP/1-only, non-replaying RoundTripper with a private,
// immutable policy. Connections are not reused, so net/http cannot retry a
// request on a failed pooled connection. It never uses DefaultTransport.
type Transport struct {
	base    *http.Transport
	config  policyConfig
	streams chan struct{}
}

// NewTransport validates all mandatory choices and snapshots proxy credentials
// and CA contents. Direct mode ignores stored proxy settings. TLS skip requires
// risk acceptance and ignores stored CA paths without opening them.
func NewTransport(policy Policy) (*Transport, error) {
	config, err := compilePolicy(policy)
	if err != nil {
		return nil, err
	}
	return newTransport(config), nil
}

// NewClient creates a private client that does not follow redirects, retry, or
// inherit host proxy/TLS settings. Its timeout bounds a complete request/body.
func NewClient(policy Policy) (*http.Client, error) {
	transport, err := NewTransport(policy)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: transport,
		Timeout:   operationTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func newTransport(config policyConfig) *Transport {
	dialer := &net.Dialer{
		Timeout:       dialTimeout,
		KeepAlive:     -1,
		FallbackDelay: -1,
	}
	base := &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) {
			return config.proxyForURL(request.URL)
		},
		DialContext:            dialer.DialContext,
		TLSClientConfig:        config.tlsConfig,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ResponseHeaderTimeout:  headerTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		ForceAttemptHTTP2:      false,
		TLSNextProto:           make(map[string]func(string, *tls.Conn) http.RoundTripper),
		GetProxyConnectHeader: func(context.Context, *url.URL, string) (http.Header, error) {
			header := make(http.Header)
			if config.authorization != "" {
				header.Set("Proxy-Authorization", config.authorization)
			}
			return header, nil
		},
	}
	return &Transport{base: base, config: config, streams: make(chan struct{}, maxClientStreams)}
}

func (transport *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Host == "" || request.URL.User != nil ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		return nil, errors.New("network policy: an HTTP(S) request without URL credentials is required")
	}
	proxy, err := transport.config.proxyForURL(request.URL)
	if err != nil {
		return nil, errRequestFailed
	}
	select {
	case transport.streams <- struct{}{}:
	default:
		return nil, errors.New("network policy: HTTP request capacity unavailable")
	}
	ctx, cancel := context.WithTimeout(request.Context(), operationTimeout)
	var released sync.Once
	release := func() { released.Do(func() { <-transport.streams }) }
	stop := context.AfterFunc(ctx, release)
	finish := func() {
		stop()
		release()
		cancel()
	}
	outgoing := request.Clone(ctx)
	outgoing.GetBody = nil
	outgoing.Close = true
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	stripProxyHeaders(outgoing.Header)
	if proxy != nil && outgoing.URL.Scheme == "http" && transport.config.authorization != "" {
		outgoing.Header.Set("Proxy-Authorization", transport.config.authorization)
	}
	response, err := transport.base.RoundTrip(outgoing)
	if err != nil {
		safeError := safeRequestError(ctx, err)
		finish()
		return nil, safeError
	}
	if proxy != nil && response.StatusCode == http.StatusProxyAuthRequired {
		finish()
		_ = response.Body.Close()
		return nil, errProxyRejected
	}
	stripProxyHeaders(response.Header)
	response.Request = request.Clone(request.Context())
	stripProxyHeaders(response.Request.Header)
	response.Status = strconv.Itoa(response.StatusCode) + " " + http.StatusText(response.StatusCode)
	response.Body = &responseBody{ReadCloser: response.Body, ctx: ctx, finish: finish}
	return response, nil
}

// CACertSHA256 returns the lowercase hexadecimal SHA-256 of the exact custom
// CA file bytes loaded at construction. It never rereads a path and is empty
// for system trust or TLS skip, which does not load a retained custom CA.
func (transport *Transport) CACertSHA256() string { return transport.config.caCertSHA256 }

func (transport *Transport) CloseIdleConnections() { transport.base.CloseIdleConnections() }
func (*Transport) String() string                  { return "netpolicy.Transport" }
func (*Transport) GoString() string                { return "netpolicy.Transport" }

type responseBody struct {
	io.ReadCloser
	ctx    context.Context
	finish func()
}

func (body *responseBody) Read(data []byte) (int, error) {
	n, err := body.ReadCloser.Read(data)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			err = safeRequestError(body.ctx, err)
		}
		body.finish()
	}
	return n, err
}

func (body *responseBody) Close() error {
	body.finish()
	if err := body.ReadCloser.Close(); err != nil {
		return errRequestFailed
	}
	return nil
}

func safeRequestError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	// Proxy response reason phrases and parser errors can contain credentials.
	return errRequestFailed
}

func stripProxyHeaders(header http.Header) {
	header.Del("Proxy-Authorization")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Connection")
}

func compilePolicy(policy Policy) (policyConfig, error) {
	if policy.ProxyMode != "direct" && policy.ProxyMode != "proxy" {
		return policyConfig{}, errors.New("network policy: explicitly select direct or proxy mode")
	}
	if policy.TLSMode != "verify" && policy.TLSMode != "skip" {
		return policyConfig{}, errors.New("network policy: explicitly select verify or skip TLS mode")
	}
	if policy.TrustMode != "system" && policy.TrustMode != "custom" {
		return policyConfig{}, errors.New("network policy: explicitly select system or custom trust mode")
	}
	if policy.TLSMode == "skip" && !policy.TLSRiskAccepted {
		return policyConfig{}, errors.New("network policy: TLS skip requires confirmed risk acceptance")
	}

	proxySettings := httpproxy.Config{NoProxy: policy.NoProxy}
	authorization := ""
	if policy.ProxyMode == "proxy" {
		proxyURL, err := parseProxyURL(policy.ProxyURL)
		if err != nil {
			return policyConfig{}, err
		}
		if len(policy.ProxyTargets) == 0 {
			return policyConfig{}, errors.New("network policy: proxy targets must be explicitly selected")
		}
		for _, target := range policy.ProxyTargets {
			switch target {
			case "http":
				proxySettings.HTTPProxy = proxyURL.String()
			case "https":
				proxySettings.HTTPSProxy = proxyURL.String()
			case "all":
				proxySettings.HTTPProxy = proxyURL.String()
				proxySettings.HTTPSProxy = proxyURL.String()
			default:
				return policyConfig{}, errors.New("network policy: proxy targets must be http, https, or all")
			}
		}
		if policy.Auth != nil {
			username, password := policy.Auth.Username, policy.Auth.Password
			if username == "" || len(username)+len(password) > maxAuthBytes || strings.ContainsAny(username, ":\r\n") {
				return policyConfig{}, errors.New("network policy: invalid proxy credentials")
			}
			authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		}
	}

	roots := x509.NewCertPool()
	caCertSHA256 := ""
	if policy.TLSMode == "verify" {
		systemRoots, err := loadSystemRoots()
		if err != nil || systemRoots == nil {
			return policyConfig{}, errors.New("network policy: cannot load system certificate trust")
		}
		roots = systemRoots.Clone()
		if policy.TrustMode == "custom" {
			if policy.CACertPath == "" {
				return policyConfig{}, errors.New("network policy: custom trust requires a CA certificate file")
			}
			file, err := os.Open(policy.CACertPath)
			if err != nil {
				return policyConfig{}, errors.New("network policy: cannot read CA certificate file")
			}
			info, statErr := file.Stat()
			if statErr != nil || !info.Mode().IsRegular() {
				_ = file.Close()
				return policyConfig{}, errors.New("network policy: CA certificate file must be a regular file")
			}
			pem, readErr := io.ReadAll(io.LimitReader(file, maxCABytes+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil || len(pem) > maxCABytes {
				return policyConfig{}, errors.New("network policy: cannot read bounded CA certificate file")
			}
			if !roots.AppendCertsFromPEM(pem) {
				return policyConfig{}, errors.New("network policy: CA certificate file contains no valid certificates")
			}
			digest := sha256.Sum256(pem)
			caCertSHA256 = hex.EncodeToString(digest[:])
		}
	}
	return policyConfig{
		proxyForURL:   proxySettings.ProxyFunc(),
		authorization: authorization,
		caCertSHA256:  caCertSHA256,
		tlsConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			RootCAs:            roots,
			InsecureSkipVerify: policy.TLSMode == "skip", // Explicit, confirmed policy only.
			NextProtos:         []string{"http/1.1"},
		},
	}, nil
}

func parseProxyURL(value string) (*url.URL, error) {
	proxyURL, err := url.Parse(value)
	if err != nil || proxyURL == nil || proxyURL.Hostname() == "" ||
		(proxyURL.Scheme != "http" && proxyURL.Scheme != "https") ||
		proxyURL.User != nil || proxyURL.Path != "" || proxyURL.RawPath != "" ||
		proxyURL.RawQuery != "" || proxyURL.ForceQuery || strings.Contains(value, "#") ||
		proxyURL.Opaque != "" || strings.ContainsAny(proxyURL.Host, "\r\n\t ") ||
		strings.HasSuffix(proxyURL.Host, ":") {
		return nil, errors.New("network policy: proxy URL must be HTTP(S) with only a host and optional port")
	}
	if port := proxyURL.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("network policy: invalid proxy port")
		}
	}
	return proxyURL, nil
}

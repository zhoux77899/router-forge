package netpolicy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"time"
)

const maxInformationalResponses = 8

func (relay *ProxyRelay) forwardHTTP(ctx context.Context, request *http.Request) (*http.Response, error) {
	proxy, err := relay.transport.config.proxyForURL(request.URL)
	if err != nil {
		return nil, errRequestFailed
	}
	peer := request.URL
	if proxy != nil {
		peer = proxy
	}
	raw, err := relay.transport.base.DialContext(ctx, "tcp", proxyAddress(peer))
	if err != nil {
		return nil, errRequestFailed
	}
	conn := raw
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	ready := false
	defer func() {
		if !ready {
			stop()
			_ = raw.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if peer.Scheme == "https" {
		config := relay.transport.config.tlsConfig.Clone()
		config.ServerName = peer.Hostname()
		secure := tls.Client(conn, config)
		handshakeCtx, cancel := context.WithTimeout(ctx, tlsHandshakeTimeout)
		err := secure.HandshakeContext(handshakeCtx)
		cancel()
		if err != nil {
			return nil, errRequestFailed
		}
		conn = secure
	}
	outgoing := request.Clone(ctx)
	outgoing.Close = true
	outgoing.GetBody = nil
	if proxy != nil {
		if relay.transport.config.authorization != "" {
			outgoing.Header.Set("Proxy-Authorization", relay.transport.config.authorization)
		}
		err = outgoing.WriteProxy(conn)
	} else {
		err = outgoing.Write(conn)
	}
	if err != nil {
		return nil, errRequestFailed
	}
	headerDeadline := time.Now().Add(headerTimeout)
	if deadline.Before(headerDeadline) {
		headerDeadline = deadline
	}
	_ = conn.SetDeadline(headerDeadline)
	limited := &headerReader{reader: conn, remaining: maxHeaderBytes, limited: true}
	buffered := bufio.NewReader(limited)
	for range maxInformationalResponses {
		response, err := readRelayResponse(buffered, outgoing)
		if err != nil {
			return nil, errRequestFailed
		}
		if response.StatusCode == http.StatusSwitchingProtocols {
			return nil, errRequestFailed
		}
		if response.StatusCode < 200 {
			continue
		}
		if proxy != nil && response.StatusCode == http.StatusProxyAuthRequired {
			return nil, errProxyRejected
		}
		limited.limited = false
		_ = conn.SetDeadline(deadline)
		stripRelayHeaders(response.Header)
		response.Request = request
		response.Body = &responseBody{
			ReadCloser: response.Body,
			ctx:        ctx,
			finish: func() {
				stop()
				_ = raw.Close()
			},
		}
		ready = true
		return response, nil
	}
	return nil, errRequestFailed
}

func proxyAddress(peer *url.URL) string {
	port := peer.Port()
	if port == "" {
		if peer.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(peer.Hostname(), port)
}

func readRelayResponse(reader *bufio.Reader, request *http.Request) (*http.Response, error) {
	var header bytes.Buffer
	statusLineLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, errRequestFailed
		}
		if statusLineLength == 0 {
			statusLineLength = len(line)
		}
		header.WriteString(line)
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	headers, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(header.Bytes()[statusLineLength:]))).ReadMIMEHeader()
	if err != nil {
		return nil, errRequestFailed
	}
	responseReader := bufio.NewReader(io.MultiReader(bytes.NewReader(header.Bytes()), reader))
	response, err := http.ReadResponse(responseReader, request)
	if err != nil {
		return nil, errRequestFailed
	}
	// ReadResponse removes Connection if it contains close. Preserve nominations
	// until the relay has removed the named hop headers as well.
	for _, key := range []string{"Connection", "Proxy-Connection"} {
		if values := headers.Values(key); len(values) != 0 {
			response.Header[key] = values
		}
	}
	return response, nil
}

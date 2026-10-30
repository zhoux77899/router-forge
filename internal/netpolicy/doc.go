// Package netpolicy compiles explicit, domain-independent network choices into
// frozen HTTP transports and operation-scoped authenticated proxy relays.
//
// ProxyMode (direct/proxy), TLSMode (verify/skip), and TrustMode (system/custom)
// are mandatory. Proxy mode additionally requires one credential-free HTTP(S)
// ProxyURL and explicit http/https/all targets. NO_PROXY is interpreted by
// golang.org/x/net/http/httpproxy, including its standard localhost/loopback
// bypass. There is no environment-derived proxy selection or alternate proxy.
// System trust uses a private OS-store snapshot, never SSL_CERT_FILE/SSL_CERT_DIR
// or a shared x509 root cache on Windows/Unix. Unix OS-store locations implement
// the explicitly selected system trust mode, not an application CA default.
// Custom trust adds a bounded PEM file to that pool. Apple native trust fails
// closed when ambient overrides or a polluted cache prevent safe native trust.
// Confirmed TLS skip ignores the CA file, even if it is invalid.
// Transport.CACertSHA256 reports only the exact custom CA file bytes loaded at
// construction, never a later path read; system trust and TLS skip return empty.
// No transport is shared with http.DefaultTransport. HTTP/2, connection reuse,
// request replay, and client redirects are disabled.
//
// A relay URL contains no credentials. Only the configured upstream receives
// Basic proxy authentication; incoming proxy credentials, cookies, and
// hop-by-hop headers are discarded. Bind addresses must be loopback or private
// unicast IPs, and every request, including CONNECT, requires the caller's exact
// remote-IP authorization. Private binding alone never grants access. Callers
// must pass an operation/run context and close the relay when its owner ends.
// CONNECT is opaque: this policy verifies HTTPS upstream proxies, not the TLS
// performed inside a workload's tunnel. Workload TLS/CA settings remain the
// caller's responsibility.
//
// Streams, sockets, headers, credentials, CA reads, and operation durations are
// bounded. Credential formatting and JSON serialization are redacted.
// Errors omit peer-supplied reason phrases, paths, and credentials; bodies and
// headers are never logged. StripNetworkEnvironment only filters a supplied
// slice; callers must also isolate downloader configuration files and add only
// explicitly configured proxy/mirror/CA/TLS values. This package neither changes
// the host environment nor configures Docker or other shared services.
package netpolicy

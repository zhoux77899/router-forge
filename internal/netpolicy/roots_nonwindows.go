//go:build !windows

package netpolicy

import (
	"crypto/x509"
	"os"
	"runtime"
)

func loadSystemRoots() (*x509.CertPool, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		// Native Apple trust cannot be exported with crypto/x509. Fail closed if
		// an override or a previously polluted cache would replace native trust.
		if os.Getenv("SSL_CERT_FILE") != "" || os.Getenv("SSL_CERT_DIR") != "" {
			return nil, errSystemTrust
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil || len(pool.Subjects()) != 0 || pool.Equal(x509.NewCertPool()) {
			return nil, errSystemTrust
		}
		return pool.Clone(), nil
	}
	files, directories, ok := systemRootLocations(runtime.GOOS)
	if !ok {
		return nil, errSystemTrust
	}
	return snapshotSystemFiles(files, directories)
}

// These are operating-system store locations, consulted only after the user
// explicitly selects system trust. They are not profile CA defaults, enterprise
// endpoints, or locations obtained from SSL_CERT_FILE/SSL_CERT_DIR.
func systemRootLocations(goos string) ([]string, []string, bool) {
	switch goos {
	case "linux", "android":
		files := []string{
			"/etc/ssl/certs/ca-certificates.crt",
			"/etc/pki/tls/certs/ca-bundle.crt",
			"/etc/ssl/ca-bundle.pem",
			"/etc/pki/tls/cacert.pem",
			"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
			"/etc/ssl/cert.pem",
		}
		directories := []string{"/etc/ssl/certs", "/etc/pki/tls/certs"}
		if goos == "android" {
			directories = append(directories, "/system/etc/security/cacerts", "/data/misc/keychain/certs-added")
		}
		return files, directories, true
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		return []string{
			"/usr/local/etc/ssl/cert.pem", "/etc/ssl/cert.pem",
			"/usr/local/share/certs/ca-root-nss.crt", "/etc/openssl/certs/ca-certificates.crt",
		}, []string{"/etc/ssl/certs", "/usr/local/share/certs", "/etc/openssl/certs"}, true
	case "solaris", "illumos":
		return []string{
			"/etc/certs/ca-certificates.crt", "/etc/ssl/certs/ca-certificates.crt", "/etc/ssl/cacert.pem",
		}, []string{"/etc/certs/CA"}, true
	case "aix":
		return []string{"/var/ssl/certs/ca-bundle.crt"}, []string{"/var/ssl/certs"}, true
	case "plan9":
		return []string{"/sys/lib/tls/ca.pem"}, nil, true
	case "js", "wasip1":
		return nil, nil, true
	default:
		return nil, nil, false
	}
}

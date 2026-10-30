package netpolicy

import (
	"crypto/x509"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func loadSystemRoots() (*x509.CertPool, error) {
	name, err := windows.UTF16PtrFromString("ROOT")
	if err != nil {
		return nil, errSystemTrust
	}
	flags := uint32(windows.CERT_SYSTEM_STORE_CURRENT_USER | windows.CERT_STORE_READONLY_FLAG | windows.CERT_STORE_OPEN_EXISTING_FLAG)
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0, flags, uintptr(unsafe.Pointer(name)))
	if err != nil {
		return nil, errSystemTrust
	}
	defer windows.CertCloseStore(store, 0)
	snapshot := newRootSnapshot()
	var current *windows.CertContext
	defer func() {
		if current != nil {
			_ = windows.CertFreeCertificateContext(current)
		}
	}()
	for {
		next, err := windows.CertEnumCertificatesInStore(store, current)
		// Enumeration frees the previous context, including at end-of-store.
		current = nil
		if errors.Is(err, syscall.Errno(windows.CRYPT_E_NOT_FOUND)) {
			return snapshot.pool, nil
		}
		if err != nil || next == nil {
			return nil, errSystemTrust
		}
		current = next
		if current.EncodedCert == nil || current.Length == 0 || current.Length > maxCABytes {
			return nil, errSystemTrust
		}
		encoded := append([]byte(nil), unsafe.Slice(current.EncodedCert, int(current.Length))...)
		if err := snapshot.addDER(encoded); err != nil {
			return nil, err
		}
	}
}

package netpolicy

import (
	"crypto/x509"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const (
	maxSystemRootBytes   = 64 << 20
	maxSystemRootEntries = 4096
)

var errSystemTrust = errors.New("network policy: cannot snapshot operating-system certificate trust")

type rootSnapshot struct {
	pool      *x509.CertPool
	remaining int
	entries   int
}

func newRootSnapshot() *rootSnapshot {
	return &rootSnapshot{pool: x509.NewCertPool(), remaining: maxSystemRootBytes}
}

func (snapshot *rootSnapshot) addDER(encoded []byte) error {
	if snapshot.entries >= maxSystemRootEntries || len(encoded) > maxCABytes || len(encoded) > snapshot.remaining {
		return errSystemTrust
	}
	snapshot.entries++
	snapshot.remaining -= len(encoded)
	certificate, err := x509.ParseCertificate(encoded)
	if err != nil {
		return errSystemTrust
	}
	snapshot.pool.AddCert(certificate)
	return nil
}

func (snapshot *rootSnapshot) readPEM(path string) (bool, error) {
	if snapshot.entries >= maxSystemRootEntries {
		return false, errSystemTrust
	}
	snapshot.entries++
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errSystemTrust
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	if info.Size() > maxCABytes || info.Size() > int64(snapshot.remaining) {
		return false, errSystemTrust
	}
	file, err := os.Open(path)
	if err != nil {
		return false, errSystemTrust
	}
	limit := min(maxCABytes, snapshot.remaining)
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > limit {
		return false, errSystemTrust
	}
	snapshot.remaining -= len(data)
	snapshot.pool.AppendCertsFromPEM(data)
	return true, nil
}

func snapshotSystemFiles(files, directories []string) (*x509.CertPool, error) {
	snapshot := newRootSnapshot()
	for _, path := range files {
		read, err := snapshot.readPEM(path)
		if err != nil {
			return nil, err
		}
		if read {
			break
		}
	}
	for _, path := range directories {
		directory, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, errSystemTrust
		}
		readErr := snapshot.readDirectory(directory, path)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			return nil, errSystemTrust
		}
	}
	return snapshot.pool, nil
}

func (snapshot *rootSnapshot) readDirectory(directory *os.File, path string) error {
	for {
		entries, err := directory.ReadDir(128)
		for _, entry := range entries {
			if snapshot.entries >= maxSystemRootEntries {
				return errSystemTrust
			}
			if entry.IsDir() {
				snapshot.entries++
				continue
			}
			if _, err := snapshot.readPEM(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errSystemTrust
		}
	}
}

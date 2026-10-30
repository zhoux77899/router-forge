//go:build !windows

package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseAndJournalModes(t *testing.T) {
	s := testStore(t)
	for _, path := range []string{s.DataDir, filepath.Join(s.DataDir, "router-forge.sqlite"), filepath.Join(s.DataDir, "router-forge.sqlite-wal"), filepath.Join(s.DataDir, "router-forge.sqlite-shm")} {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s permissions %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

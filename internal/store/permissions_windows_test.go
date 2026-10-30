//go:build windows

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDatabaseAndJournalDACLs(t *testing.T) {
	s := testStore(t)
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.DataDir, filepath.Join(s.DataDir, "router-forge.sqlite"), filepath.Join(s.DataDir, "router-forge.sqlite-wal"), filepath.Join(s.DataDir, "router-forge.sqlite-shm")} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		text := sd.String()
		if !strings.Contains(text, user.User.Sid.String()) {
			t.Fatalf("current user missing in %s: %s", path, text)
		}
		for _, broad := range []string{";;;WD)", ";;;AU)", ";;;BU)"} {
			if strings.Contains(text, broad) {
				t.Fatalf("broad access on %s: %s", path, text)
			}
		}
	}
}

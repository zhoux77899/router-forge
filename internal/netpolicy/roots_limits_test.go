package netpolicy

import (
	"errors"
	"net/http"
	"testing"
)

func TestRootSnapshotBoundsAndPrivatePools(t *testing.T) {
	server := newTLSFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	path := certificateFile(t, server)
	for _, limited := range []*rootSnapshot{
		{pool: newRootSnapshot().pool, remaining: 1},
		{pool: newRootSnapshot().pool, remaining: maxSystemRootBytes, entries: maxSystemRootEntries},
	} {
		if _, err := limited.readPEM(path); !errors.Is(err, errSystemTrust) {
			t.Fatal("system root snapshot resource cap was not enforced")
		}
		if err := limited.addDER(server.Certificate().Raw); !errors.Is(err, errSystemTrust) {
			t.Fatal("native system root snapshot resource cap was not enforced")
		}
	}
	first, err := snapshotSystemFiles([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := snapshotSystemFiles(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Equal(second) || len(second.Subjects()) != 0 {
		t.Fatal("system root snapshot changed another pool")
	}
	other := newTLSFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	first.AddCert(other.Certificate())
	third, err := snapshotSystemFiles([]string{path}, nil)
	if err != nil || len(third.Subjects()) != 1 {
		t.Fatal("a new system snapshot inherited another pool's mutation")
	}
}

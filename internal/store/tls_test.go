package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func TestEnvironmentTLSPolicyPersistsAndRestoresCA(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "tls-profile")
	s, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	environment, err := s.SaveEnvironment(ctx, domain.Environment{Name: "TLS opt-in", Type: "local", CACertPath: filepath.Join(directory, "missing-ca.pem"), SkipTLSVerification: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := s.Environment(ctx, environment.ID)
	if err != nil || !persisted.SkipTLSVerification || persisted.CACertPath != environment.CACertPath {
		t.Fatal("TLS opt-in and retained CA did not survive reopen", err)
	}
	persisted.SkipTLSVerification = false
	strict, err := s.SaveEnvironment(ctx, persisted)
	if err != nil || strict.CACertPath != environment.CACertPath || strict.CreatedAt != environment.CreatedAt {
		t.Fatal("disabling TLS skip lost the retained CA or profile identity", err)
	}
	profiles, err := s.Environments(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].SkipTLSVerification || profiles[0].CACertPath != environment.CACertPath {
		t.Fatal("strict policy did not persist", err)
	}
}

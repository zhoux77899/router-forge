package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB      *sqlx.DB
	DataDir string
}

func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("application directory must be a real directory")
	}
	return restrictPath(path, true)
}

func EnsurePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return createErr
		}
		if err := file.Close(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("application file must be a regular file")
	}
	return restrictPath(path, false)
}

func WritePrivateFile(path string, data []byte) error {
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing to replace a non-regular application file")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = restrictPath(tmp, false); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return restrictPath(path, false)
}

func Open(dataDir string) (*Store, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err = EnsurePrivateDir(abs); err != nil {
		return nil, fmt.Errorf("protect application directory: %w", err)
	}
	path := filepath.Join(abs, "router-forge.sqlite")
	if err = EnsurePrivateFile(path); err != nil {
		return nil, fmt.Errorf("protect database: %w", err)
	}
	db, err := sqlx.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	ctx := context.Background()
	if err = db.PingContext(ctx); err != nil {
		return fail(err)
	}
	migrationFS, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fail(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db.DB, migrationFS)
	if err != nil {
		return fail(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		return fail(err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if _, err = os.Stat(name); err == nil {
			if err = restrictPath(name, false); err != nil {
				return fail(err)
			}
		}
	}
	s := &Store{DB: db, DataDir: abs}
	if err = s.seed(ctx); err != nil {
		return fail(err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

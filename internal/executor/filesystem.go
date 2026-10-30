package executor

import (
	"errors"
	"os"
	"path/filepath"
)

func writePrivate(filename string, data []byte) error {
	if err := privateDir(filepath.Dir(filename)); err != nil {
		return err
	}
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private file cannot be a symlink")
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = securePath(filename, false); err != nil {
		f.Close()
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

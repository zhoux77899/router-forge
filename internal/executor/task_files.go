package executor

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func privateTaskRoot(attemptDir, taskDir string) (string, error) {
	if !filepath.IsAbs(attemptDir) || !filepath.IsAbs(taskDir) || !pathWithin(attemptDir, taskDir) {
		return "", errors.New("native task path escapes the private attempt")
	}
	root, rootErr := filepath.EvalSymlinks(attemptDir)
	task, err := filepath.EvalSymlinks(taskDir)
	if rootErr != nil || err != nil || task == root || !pathWithin(root, task) {
		return "", errors.New("resolved native task path escapes the private attempt")
	}
	info, err := os.Stat(task)
	if err != nil || !info.IsDir() {
		return "", errors.New("native task root is not an existing private directory")
	}
	return task, nil
}

func readTaskSourceFile(root, relative string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(root) || !validText(root) || relative == "" || strings.Contains(relative, "\\") || filepath.IsAbs(relative) || limit < 1 || limit > 2<<20 {
		return nil, errors.New("invalid native task source file")
	}
	filename := filepath.Join(root, filepath.FromSlash(relative))
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolved, err := filepath.EvalSymlinks(filename)
	if rootErr != nil || err != nil || !pathWithin(resolvedRoot, resolved) {
		return nil, errors.New("native task source file is missing or escapes the downloaded task")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("native task source is not a bounded regular file")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("native task source file is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("native task source file is incomplete or exceeds its limit")
	}
	return data, nil
}

package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type writeCloser struct{ io.Writer }

func (writeCloser) Close() error { return nil }
func TestVersionIsOnlyStdoutOutput(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run([]string{"--version"}, io.NopCloser(strings.NewReader("")), writeCloser{&out}, &stderr)
	if code != 0 || out.String() != domain.Version+"\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}
func TestInvalidFlagsNeverWriteWireStdout(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "--stdio"}, {"--unknown"}, {"--version", "extra"}} {
		var out, stderr bytes.Buffer
		if run(args, io.NopCloser(strings.NewReader("")), writeCloser{&out}, &stderr) == 0 || out.Len() != 0 {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}

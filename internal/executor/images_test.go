package executor

import (
	"reflect"
	"strings"
	"testing"
)

func TestNativeDockerfileOnlyPinsOriginalBaseAndKeepsTestbedBuild(t *testing.T) {
	original := []byte("FROM ghcr.io/fixture/swe-testbed:latest AS testbed\nWORKDIR /testbed\nRUN echo fixture\nFROM testbed AS final\n")
	pinned, digests, err := FreezeDockerfile(original, func(reference string) (string, error) {
		if reference != "ghcr.io/fixture/swe-testbed:latest" {
			t.Errorf("wrong original image: %s", reference)
		}
		return "ghcr.io/fixture/swe-testbed@sha256:" + strings.Repeat("a", 64), nil
	})
	expected := "FROM ghcr.io/fixture/swe-testbed@sha256:" + strings.Repeat("a", 64) + " AS testbed\nWORKDIR /testbed\nRUN echo fixture\nFROM testbed AS final\n"
	if err != nil || string(pinned) != expected || !reflect.DeepEqual(digests, []string{"ghcr.io/fixture/swe-testbed@sha256:" + strings.Repeat("a", 64)}) {
		t.Fatalf("native pin = %s %#v %v", pinned, digests, err)
	}
	for _, bad := range []string{"FROM $PRIVATE_IMAGE\n", "FROM alpine:latest\nCOPY --from=other:latest /foo /foo\n"} {
		if _, _, err := FreezeDockerfile([]byte(bad), func(string) (string, error) { return "alpine@sha256:" + strings.Repeat("a", 64), nil }); err == nil {
			t.Fatal("unfrozen dynamic/remote image accepted")
		}
	}
}
func TestPinnedTaskMetadataPreservesOriginalVerifierTimeout(t *testing.T) {
	metadata, err := parseTaskMetadata([]byte("version = \"1.0\"\n[environment]\ndocker_image = 'ghcr.io/fixture/testbed:latest'\nbuild_timeout_sec = 900\n[verifier]\ntimeout_sec = 1200.0\n"))
	if err != nil || metadata.Image != "ghcr.io/fixture/testbed:latest" || metadata.VerifierTimeoutSec != 1200 || metadata.BuildTimeoutSec != 900 || metadata.VerifierMode != "shared" {
		t.Fatalf("metadata = %#v %v", metadata, err)
	}
}

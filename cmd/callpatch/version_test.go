package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersionLockstep asserts every version surface reports the same version:
// the VERSION file, the CLI `callpatch version` output, and the website
// manifest's meta.content_version. A drift between any two surfaces is how a
// release ships claiming the wrong version.
func TestVersionLockstep(t *testing.T) {
	want := "0.3.0"

	root := repoRoot(t)

	versionFile := string(bytesTrimNewline(mustRead(t, filepath.Join(root, "VERSION"))))
	assert.Equal(t, want, versionFile, "VERSION file must be %s", want)

	cliOut, err := runCallpatch(t, "version")
	require.NoErrorf(t, err, "callpatch version failed: %s", cliOut)
	assert.Contains(t, cliOut, "callpatch v"+want, "CLI version must print v%s", want)

	siteRaw := mustRead(t, filepath.Join(root, "web", "site.json"))
	var site struct {
		Meta struct {
			ContentVersion string `json:"content_version"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(siteRaw, &site), "web/site.json must stay valid JSON")
	assert.Equal(t, want, site.Meta.ContentVersion, "web/site.json meta.content_version must be %s", want)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}

func bytesTrimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

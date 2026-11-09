package webui

import (
	"io/fs"
	"testing"
)

// FS has to answer honestly in both kinds of build: a development binary carries no
// assets (nil), a release binary carries a client whose entry point is index.html.
func TestFSDescribesTheBuildItWasCompiledInto(t *testing.T) {
	fsys := FS()
	if fsys == nil {
		return // built without scripts/build.sh; --web-dir serves the client instead
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Fatalf("assets are embedded but index.html is missing: %v", err)
	}
}

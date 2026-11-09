// Package webui carries the built single page client inside the binary.
//
// The frontend and the daemon are separate projects: client/ builds with npm and
// knows nothing about Go, the daemon knows nothing about npm. They meet exactly once,
// in scripts/build.sh, which copies client/dist here before compiling. A binary built
// without that step has no assets and says so (FS returns nil), so a developer can
// still run the daemon against a live client directory with --web-dir.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// FS returns the embedded client, or nil when this binary was built without one.
//
// The empty case matters: a .gitkeep keeps the directory compilable, and a daemon that
// mistook an empty embed for a real client would serve 404s from a healthy-looking
// process.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil || len(entries) == 0 {
		return nil
	}
	if len(entries) == 1 && entries[0].Name() == ".gitkeep" {
		return nil
	}
	return sub
}

// Package web embeds the built React frontend into the Go binary so the
// server can ship as a single self-contained executable.
package web

import (
	"embed"
	"errors"
	"io/fs"
)

// distFS holds the contents of web/dist after running `npm run build`.
// The "all:" prefix ensures hidden files (e.g. .vite metadata) are included.
//
//go:embed all:dist
var distFS embed.FS

// UI returns the embedded frontend filesystem rooted at dist/. It returns an
// error when the frontend has not been built yet so the server can still
// boot in API-only mode during development.
func UI() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, errors.New("frontend dist/index.html missing; run `npm --prefix web run build`")
	}
	return sub, nil
}

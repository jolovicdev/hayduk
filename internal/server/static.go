package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func distFileSystem() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // embed misconfiguration; fail loudly at startup
	}
	return sub
}

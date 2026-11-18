package web

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var static embed.FS

// staticFS exposes the embedded dashboard files at the filesystem root.
func staticFS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

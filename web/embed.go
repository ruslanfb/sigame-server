// Package web embeds the built single-page client (web/dist) into the server binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built client rooted at dist/ (index.html at the root). A
// checkout without a web build contains only .gitkeep: the HTTP layer then
// falls back to the docs landing page.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

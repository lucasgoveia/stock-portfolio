// Package web embeds the production build of the React app (web/dist).
package web

import (
	"embed"
	"io/fs"
)

// The placeholder web/dist/.gitkeep is committed so this directive compiles
// before the UI has been built (CI go job, fresh checkouts). `make build`
// fills dist with the real assets.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the built UI rooted at index.html. The FS has no index.html
// when the web app has not been built.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is always present: it is embedded above
	}
	return sub
}

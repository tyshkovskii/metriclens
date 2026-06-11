// Package web serves the compiled frontend single-page app embedded into the
// backend binary, so a single Docker image serves both the UI and the API.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist holds the Vite build output. The Docker build overwrites the committed
// placeholder index.html with the real assets before compiling. The all:
// prefix keeps files whose names begin with "_" (none today, but Vite may emit
// them) and lets `go build`/`go test` succeed without a frontend build.
//
//go:embed all:dist
var dist embed.FS

// assets returns the embedded build rooted at the dist directory.
func assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Only happens if the embed directive is broken at build time.
		panic(err)
	}
	return sub
}

// Handler serves the embedded SPA. Requests for real files (index.html, hashed
// JS/CSS under /assets) are served directly; any other path falls back to
// index.html so client-side routes such as /targets/:id resolve on reload.
func Handler() http.Handler {
	files := assets()
	fileServer := http.FileServer(http.FS(files))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" {
			clean = "index.html"
		}

		if _, err := fs.Stat(files, clean); err != nil {
			// Not a bundled asset: serve the app shell for SPA routing.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}

package web

import (
	"bytes"
	"io/fs"
	"testing"
)

// The embedded frontend must be functional even when building Go directly
// (without running Vite first). Prevent placeholder bundles in releases.
func TestEmbeddedFrontendBundle(t *testing.T) {
	js, err := fs.ReadFile(FS, "static/dist/assets/main.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(js) < 2048 ||
		bytes.Contains(js, []byte("Placeholder build artifact")) ||
		!bytes.Contains(js, []byte("createForm")) ||
		!bytes.Contains(js, []byte("/api/links")) {
		t.Fatal("embedded frontend JavaScript is missing or a placeholder")
	}

	css, err := fs.ReadFile(FS, "static/dist/assets/main.css")
	if err != nil {
		t.Fatal(err)
	}
	if len(css) < 2048 ||
		bytes.Contains(css, []byte("Placeholder build artifact")) ||
		!bytes.Contains(css, []byte(".bg-white")) {
		t.Fatal("embedded frontend CSS is missing or a placeholder")
	}
}

package httpapi

import (
	_ "embed"
	"net/http"
)

// indexHTML is the single-page UI, embedded into the binary at build time so the
// service ships as one self-contained artifact.
//
//go:embed index.html
var indexHTML []byte

// serveIndex serves the IP-lookup web page. It is hosted by the same server as
// the API, so the page's fetch() calls are same-origin and need no CORS.
func serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

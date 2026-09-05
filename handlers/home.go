package handlers

import (
	"embed"
	"net/http"
)

//go:embed home/index.html home/home.css
var homeFiles embed.FS

// HomeHandler serves only the public homepage and its stylesheet. Keeping the
// stylesheet under a nested path leaves every single-segment redirect intact.
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	name, contentType := "home/index.html", "text/html; charset=utf-8"
	switch r.URL.Path {
	case "/":
	case "/.well-known/home.css":
		name, contentType = "home/home.css", "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// These embedded resources contain no session-specific data. A short cache
	// lifetime avoids stale unversioned styling across releases.
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	content, err := homeFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}

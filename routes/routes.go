package routes

import (
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/handlers"
	"net/http"
)

func NewRouter(database *db.DbWrapper) *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("/.well-known/healthz", handlers.HealthHandler)
	router.HandleFunc("/.well-known/readyz", handlers.ReadinessHandler(database))
	router.HandleFunc("/{slug}", handlers.RedirectHandler(database))
	router.HandleFunc("/", handlers.HelloHandler)
	return router
}

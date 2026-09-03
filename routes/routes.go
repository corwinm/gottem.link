package routes

import (
	"net/http"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/handlers"
)

func NewRouter(database *db.DbWrapper, managementToken string) *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("/.well-known/healthz", handlers.HealthHandler)
	router.HandleFunc("/.well-known/readyz", handlers.ReadinessHandler(database))
	router.HandleFunc("/api/", http.NotFound)
	if managementToken != "" {
		router.Handle("/api/v1/redirects", handlers.BearerAuth(managementToken, handlers.ManagementCollectionHandler(database)))
		router.Handle("/api/v1/redirects/{slug}", handlers.BearerAuth(managementToken, handlers.ManagementItemHandler(database)))
		router.Handle("/api/v1/redirects/{slug}/disable", handlers.BearerAuth(managementToken, handlers.ManagementDisableHandler(database)))
	}
	router.HandleFunc("/{slug}", handlers.RedirectHandler(database))
	router.HandleFunc("/", handlers.HelloHandler)
	return router
}

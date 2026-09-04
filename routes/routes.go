package routes

import (
	"net/http"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/handlers"
)

func NewRouter(database *db.DbWrapper, managementToken string) *http.ServeMux {
	return newRouter(database, managementToken, handlers.GenerateSlug)
}

func NewRouterWithBackupToken(database *db.DbWrapper, managementToken, backupToken string) *http.ServeMux {
	return newRouterWithBackupToken(database, managementToken, backupToken, handlers.GenerateSlug)
}

func newRouter(database *db.DbWrapper, managementToken string, generateSlug handlers.SlugGenerator) *http.ServeMux {
	return newRouterWithBackupToken(database, managementToken, "", generateSlug)
}

func newRouterWithBackupToken(database *db.DbWrapper, managementToken, backupToken string, generateSlug handlers.SlugGenerator) *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("/.well-known/healthz", handlers.HealthHandler)
	router.HandleFunc("/.well-known/readyz", handlers.ReadinessHandler(database))
	router.HandleFunc("/api/", http.NotFound)
	if managementToken != "" || backupToken != "" {
		router.Handle("/api/v1/exports", handlers.BearerAuthAny([]string{managementToken, backupToken}, handlers.ManagementExportHandler(database)))
	}
	if managementToken != "" {
		router.Handle("/api/v1/imports", handlers.BearerAuth(managementToken, handlers.ManagementImportHandler(database)))
		router.Handle("/api/v1/redirects", handlers.BearerAuth(managementToken, handlers.ManagementCollectionHandler(database, generateSlug)))
		router.Handle("/api/v1/redirects/{slug}", handlers.BearerAuth(managementToken, handlers.ManagementItemHandler(database)))
		router.Handle("/api/v1/redirects/{slug}/disable", handlers.BearerAuth(managementToken, handlers.ManagementDisableHandler(database)))
	}
	router.HandleFunc("/{slug}", handlers.RedirectHandler(database))
	router.HandleFunc("/", handlers.HelloHandler)
	return router
}

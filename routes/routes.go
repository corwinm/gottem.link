package routes

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"corwinm/gottem.link/db"
	"corwinm/gottem.link/handlers"
)

type AdminConfig struct {
	Origin        string
	SessionSecret string
	SecureCookies bool
}

func NewRouter(database *db.DbWrapper, managementToken string) *http.ServeMux {
	return newRouter(database, managementToken, handlers.GenerateSlug)
}

func NewRouterWithBackupToken(database *db.DbWrapper, managementToken, backupToken string) *http.ServeMux {
	return newRouterWithBackupToken(database, managementToken, backupToken, handlers.GenerateSlug)
}

func NewRouterWithAdmin(database *db.DbWrapper, managementToken, backupToken string, admin AdminConfig) *http.ServeMux {
	return newRouterWithAdmin(database, managementToken, backupToken, admin, handlers.GenerateSlug)
}

func newRouter(database *db.DbWrapper, managementToken string, generateSlug handlers.SlugGenerator) *http.ServeMux {
	return newRouterWithBackupToken(database, managementToken, "", generateSlug)
}

func newRouterWithBackupToken(database *db.DbWrapper, managementToken, backupToken string, generateSlug handlers.SlugGenerator) *http.ServeMux {
	return newRouterWithAdmin(database, managementToken, backupToken, AdminConfig{}, generateSlug)
}

func newRouterWithAdmin(database *db.DbWrapper, managementToken, backupToken string, admin AdminConfig, generateSlug handlers.SlugGenerator) *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("/.well-known/healthz", handlers.HealthHandler)
	router.HandleFunc("/.well-known/readyz", handlers.ReadinessHandler(database))
	router.HandleFunc("/api/", http.NotFound)
	router.HandleFunc("/admin/", http.NotFound)

	adminEnabled := managementToken != "" && validAdminConfig(admin)
	var managementAuth func(http.Handler) http.Handler
	if adminEnabled {
		sessions := handlers.NewSessionAuth(managementToken, admin.SessionSecret, admin.Origin, admin.SecureCookies, nil)
		managementAuth = sessions.BrowserOrBearerAuth
		router.Handle("/api/v1/session", sessions.SessionHandler())
		router.Handle("/admin", handlers.AdminPageOrLegacyRedirectHandler(database))
		router.Handle("/admin/{$}", handlers.AdminPageHandler())
		router.Handle("/admin/assets/admin.css", handlers.AdminAssetHandler("admin.css", "text/css; charset=utf-8"))
		router.Handle("/admin/assets/admin.js", handlers.AdminAssetHandler("admin.js", "text/javascript; charset=utf-8"))
	} else {
		managementAuth = func(next http.Handler) http.Handler { return handlers.BearerAuth(managementToken, next) }
		router.Handle("/admin", handlers.AdminLegacyRedirectHandler(database))
	}

	if managementToken != "" || backupToken != "" {
		router.Handle("/api/v1/exports", handlers.BearerAuthAny([]string{managementToken, backupToken}, handlers.ManagementExportHandler(database)))
	}
	if managementToken != "" {
		router.Handle("/api/v1/imports", managementAuth(handlers.ManagementImportHandler(database)))
		router.Handle("/api/v1/redirects", managementAuth(handlers.ManagementCollectionHandler(database, generateSlug)))
		router.Handle("/api/v1/redirects/{slug}", managementAuth(handlers.ManagementItemHandler(database)))
		router.Handle("/api/v1/redirects/{slug}/disable", managementAuth(handlers.ManagementDisableHandler(database)))
		router.Handle("/api/v1/redirects/{slug}/enable", managementAuth(handlers.ManagementEnableHandler(database)))
	}
	router.HandleFunc("/{slug}", handlers.RedirectHandler(database))
	router.HandleFunc("/", handlers.HelloHandler)
	return router
}

func validAdminConfig(admin AdminConfig) bool {
	if len(admin.SessionSecret) < 32 {
		return false
	}
	origin, err := url.Parse(admin.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Opaque != "" {
		return false
	}
	if admin.Origin != origin.Scheme+"://"+origin.Host {
		return false
	}
	if (origin.Scheme == "https" && origin.Port() == "443") || (origin.Scheme == "http" && origin.Port() == "80") {
		return false
	}
	if origin.Scheme == "https" {
		return admin.SecureCookies
	}
	if origin.Scheme != "http" || admin.SecureCookies {
		return false
	}
	host := origin.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

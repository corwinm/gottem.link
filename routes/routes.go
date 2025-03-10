package routes

import (
	"corwinm/gottem.link/handlers"
	"net/http"
)

func NewRouter() *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("/{slug}", handlers.RedirectHandler)
	router.HandleFunc("/", handlers.HelloHandler)
	return router
}

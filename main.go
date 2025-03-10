package main

import (
	"corwinm/gottem.link/routes"
	"log"
	"net/http"
)

func main() {
	log.Println("Server is running on http://localhost:8080")
	router := routes.NewRouter()
	log.Fatal(http.ListenAndServe(":8080", router))
}

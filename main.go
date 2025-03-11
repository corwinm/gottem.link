package main

import (
	"corwinm/gottem.link/routes"
	"flag"
	"log"
	"net/http"
)

func main() {
	router := routes.NewRouter()
	port := flag.String("addr", ":8080", "http service address")
	flag.String("dsn", "gottem.db", "Database file")
	flag.Parse()

	log.Println("Server is running on http://localhost" + *port)
	log.Fatal(http.ListenAndServe(*port, router))
}

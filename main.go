package main

import (
	"corwinm/gottem.link/db"
	"corwinm/gottem.link/routes"
	"log"
	"net/http"
	"os"
)

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	database, err := db.Open(config.dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	log.Println("Server is listening on " + config.addr)
	log.Fatal(http.ListenAndServe(config.addr, routes.NewRouter(database)))
}

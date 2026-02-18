package main

import (
	"log"
	"net/http"
)

func main() {
	relay := NewRelay()
	http.Handle("/", relay)
	log.Println("Nostr relay listening on :9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}

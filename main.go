package main

import (
	"log"

	"github.com/javgh/sia-nbdserver/nbd"
	"github.com/javgh/sia-nbdserver/sia"
)

func main() {
	siaBackend, err := sia.NewBackend(1099511627776)
	if err != nil {
		log.Fatal(err)
	}

	nbd.Playground(siaBackend)
}

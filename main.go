package main

import (
	"log"

	"payment.service/server"
)

func main() {
	if err := server.Server(); err != nil {
		log.Fatal(err)
	}
}

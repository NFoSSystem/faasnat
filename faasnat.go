package main

import (
	"faasnat/handlers"
	"log"
	"os"
)

func main() {
	log.Println("Starting UDP Nat")

	args := os.Args[1:]
	if len(args) < 1 {
		log.Fatalln("Error interface name not provided as parameter")
	}

	handlers.StartUDPNat(args)
}

package main

import (
	"log"
	"os"

	"bitbucket.org/Manaphy91/faasnat/handlers"
)

func main() {

	args := os.Args[1:]
	if len(args) < 1 {
		log.Fatalf("Error interface name not provided as parameter")
	}

	interfaceName := args[0]

	log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	go handlers.StartIPInterface()
	handlers.StartUDPNat(interfaceName)
	<-stopChan
}

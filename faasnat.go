package main

import (
	"log"

	"bitbucket.org/Manaphy91/faasnat/handlers"
)

func main() {
	log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	go handlers.StartIPInterface()
	handlers.StartUDPNat()
	<-stopChan
}

package main

import (
	"faasnat/handlers"
	"log"
)

func main() {
	log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	go handlers.StartIPInterface()
	handlers.StartUDPNat()
	<-stopChan
}

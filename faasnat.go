package main

import (
	"log"

	"github.com/NFoSSystem/faasnat/handlers"
)

func main() {
	log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	go handlers.StartIPInterface()
	handlers.StartUDPNat()
	<-stopChan
}

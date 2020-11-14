package main

import (
	"faasnat/handlers"
	"log"
)

func main() {
	log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	handlers.StartUDPNat()
	<-stopChan
}

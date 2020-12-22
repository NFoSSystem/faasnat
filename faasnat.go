package main

import (
	"log"
	"os"

	"bitbucket.org/Manaphy91/faasnat/handlers"
	"bitbucket.org/Manaphy91/faasnat/utils"
	"bitbucket.org/Manaphy91/nflib"
)

func main() {

	args := os.Args[1:]
	if len(args) < 1 {
		log.Fatalf("Error interface name not provided as parameter")
	}

	interfaceName := args[0]

	lIp, _ := nflib.GetLocalIpAddr()
	strPrefix := fmt.Sprintf("[%s] -> ", lIp.String())

	logger, err := nflib.NewRedisLogger(strPrefix, "logChan", lIp.String(), nflib.REDIS_PORT)
	if err != nil {
		log.Fatalln(err)
	}
	utils.Log = logger

	utils.Log.Println("Starting UDP Nat")
	stopChan := make(chan struct{})
	go handlers.StartIPInterface()
	handlers.StartUDPNat(interfaceName)
	<-stopChan
}

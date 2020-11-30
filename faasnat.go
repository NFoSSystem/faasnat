package main

import (
	"fmt"
	"log"

	"bitbucket.org/Manaphy91/faasnat/handlers@4c99c06a53"
	"bitbucket.org/Manaphy91/faasnat/utils@4c99c06a53"
	"bitbucket.org/Manaphy91/nflib@4c99c06a53"
)

func main() {
	// utils.Log.Println("Starting UDP Nat")
	// stopChan := make(chan struct{})
	// go handlers.StartIPInterface(1)
	// handlers.StartUDPNat(1)
	// <-stopChan
	var m map[string]interface{}
	Main(m)
}

func Main(obj map[string]interface{}) map[string]interface{} {
	lIp, _ := nflib.GetLocalIpAddr()
	strPrefix := fmt.Sprintf("[%s] -> ", lIp.String())

	logger, err := nflib.NewRedisLogger(strPrefix, "logChan", lIp.String(), nflib.REDIS_PORT)
	if err != nil {
		log.Fatalln(err)
	}
	utils.Log = logger

	utils.Log.Printf("Starting NAT NF at %s ...", lIp)

	nflib.SendPingMessageToRouter(utils.Log, utils.Log)

	utils.Log.Println("Starting accepting UDP packets ...")
	handlers.StartUDPNat(9826)

	res := make(map[string]interface{})
	return res
}

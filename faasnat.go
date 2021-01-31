package main

import (
	"fmt"
	"handlers"
	"log"
	"nflib"
	"utils"
)

func Main(obj map[string]interface{}) map[string]interface{} {
	lIp, _ := nflib.GetLocalIpAddr()
	strPrefix := fmt.Sprintf("[%s] -> ", lIp.String())

	redisIp, ok := obj["redisIp"].(string)
	if !ok {
		log.Fatalf("Error casting redisIp provided parameter as string\n")
	}

	logger, err := nflib.NewRedisLogger(strPrefix, "logChan", redisIp, 6379)
	if err != nil {
		log.Fatalln(err)
	}
	utils.Log = logger

	utils.Log.Printf("Starting NAT NF at %s ...", lIp)

	utils.Log.Println("Getting list of available ports ...")

	portsParam, ok := obj["leasedPorts"].(string)
	if !ok {
		utils.Log.Fatalln("Error reading ports from function input paramters")
	}

	ports := nflib.GetPortSliceFromString(portsParam)

	nflib.SendPingMessageToRouter("nat", utils.Log, utils.Log)

	utils.Log.Println("Starting accepting UDP packets ...")

	var outFlowMap, inFlowMap *handlers.FlowMap = handlers.NewFlowMap(), handlers.NewFlowMap()

	handlers.StartUDPNat(9826, ports, outFlowMap, inFlowMap)

	res := make(map[string]interface{})
	return res
}

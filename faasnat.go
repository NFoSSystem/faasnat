package main

import (
	// "log"
	// "nflib"
	// "utils"
	"fmt"
	"handlers"
	"log"
	"nflib"
	"strconv"
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

	//utils.Log.Printf("Starting NAT NF at %s ...", lIp)

	//utils.Log.Println("Getting list of available ports ...")

	portsParam, ok := obj["leasedPorts"].(string)
	if !ok {
		utils.Log.Fatalln("Error reading ports from function input paramters")
	}

	ports := nflib.GetPortSliceFromString(portsParam)

	//utils.Log.Println("Starting accepting UDP packets ...")

	var outFlowMap, inFlowMap *handlers.FlowMap = handlers.NewFlowMap(), handlers.NewFlowMap()

	cntIdStr, ok := obj["cntId"].(string)
	if !ok {
		utils.Log.Fatalf("Error reading cntId from function input paramters: %v - %s", obj, cntIdStr)
	}

	cntId, err := strconv.Atoi(cntIdStr)
	if err != nil {
		utils.Log.Fatalf("Error converting string %s into integer: %s", cntIdStr, err)
	}

	replStr, ok := obj["repl"].(string)
	if !ok {
		utils.Log.Fatalf("Error reading repl from function input paramters: %v - %s", obj, cntIdStr)
	}

	var repl bool

	//utils.Log.Printf("DEBUG Content of replStr as received from openwhisk %s\n", replStr)
	if replStr == "0" {
		repl = false
	} else {
		repl = true
	}

	handlers.StartUDPNat(9826, ports, outFlowMap, inFlowMap, uint16(cntId), repl)

	res := make(map[string]interface{})
	return res
}

// func Main(obj map[string]interface{}) map[string]interface{} {
// 	//strPrefix := fmt.Sprintf("[%s] -> ", lIp.String())

// 	logger, err := nflib.NewRedisLogger("nat", "logChan", "172.17.0.1", 6379)
// 	if err != nil {
// 		log.Fatalln(err)
// 	}
// 	utils.Log = logger

// 	defer func() {
// 		r := recover()
// 		if r != nil {
// 			utils.Log.Printf("Recover from panic - test")
// 		}
// 	}()

// 	utils.Log.Println("Befor panicking")

// 	panic(int(0))

// 	return obj
// }

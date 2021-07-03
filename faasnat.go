package main

import (
	// "log"
	// "nflib"
	// "utils"
	"encoding/base64"
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

	//utils.Log.Printf("Starting NAT NF at %s ...", lIp)

	//utils.Log.Println("Getting list of available ports ...")

	// portsParam, ok := obj["leasedPorts"].(string)
	// if !ok {
	// 	utils.Log.Fatalln("Error reading ports from function input paramters")
	// }

	//ports := nflib.GetPortSliceFromString(portsParam)

	//utils.Log.Println("Starting accepting UDP packets ...")
	handlers.InitNat()

	if pktParam, ok := obj["pkt"]; !ok {
		utils.Log.Printf("ERROR no parameter pkt sent to nat action! The action will return.")
	} else {
		if pktStr, ok := pktParam.(string); ok {
			pkt, err := base64.StdEncoding.DecodeString(pktStr)
			if err == nil {
				go handlers.HandlePacket(pkt)
			} else {
				utils.Log.Printf("ERROR during deconding of pkt param: %s", err)
			}
		}
	}

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

package main

// import (
// 	"log"
// 	"os"
// 	"syscall"
// 	"github.com/google/netstack/tcpip/header"
// )

// func main() {

// 	sockfd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, 0x0300)
// 	if err != nil {
// 		log.Printf("Error opening raw socket via Socket() syscall: %s\n", err)
// 		os.Exit(1)
// 	}

// 	err = syscall.BindToDevice(sockfd, "wlan0")
// 	if err != nil {
// 		log.Printf("Error during Bind() syscall: %s\n", err)
// 		os.Exit(1)
// 	}

// 	err = syscall.SetLsfPromisc("wlan0", true)
// 	if err != nil {
// 		log.Printf("Error setting interface wlan0 to promiscous mode: %s\n", err)
// 		os.Exit(1)
// 	}

// 	for {
// 		buff := make([]byte, 65535)
// 		log.Println("DEBUG 1")

// 		size, _, err := syscall.Recvfrom(sockfd, buff, 0)
// 		if err != nil {
// 			log.Printf("Error reading from raw socket: %s\n", err)
// 			continue
// 		}

// 		log.Printf("Content of packet read: %v\n", buff[:size])
// 	}
// }

import (
	"log"
	"net"
	"os"

	"github.com/google/netstack/tcpip/header"

	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/raw"
)

func main() {
	args := os.Args[1:]

	if len(args) < 1 {
		log.Fatalln("Error no interface name provided as argument!")
	}

	nif, err := net.InterfaceByName(args[0])
	if err != nil {
		log.Fatalf("Error accessing to interface %s\n", args[0])
	}

	eth, err := raw.ListenPacket(nif, 0x800, nil)
	if err != nil {
		log.Fatalf("Error opening ethernet interface: %s\n", err)
	}
	defer eth.Close()

	buff := make([]byte, nif.MTU)

	var eFrame ethernet.Frame

	for {
		size, _, err := eth.ReadFrom(buff)
		if err != nil {
			log.Fatalf("Error reading from ethernet interface: %s\n", err)
		}

		err = (&eFrame).UnmarshalBinary(buff[:size])
		if err != nil {
			log.Fatalf("Error unmarshalling received message: %s\n", err)
		}

		//log.Printf("Received ethernet packet: %v\n", eFrame)
		ipPkt := header.IPv4(eFrame.Payload)

		if ipPkt.Protocol() != 17 {
			continue
		}

		ipBuff := []byte(eFrame.Payload)
		// func (b IPv4) Payload() []byte {
		// 	return b[b.HeaderLength():][:b.PayloadLength()]
		// }

		udpPkt := header.UDP(ipBuff[ipPkt.HeaderLength():])

		//if udpPkt.DestinationPort() == 5000 {
		log.Printf("Received packet from %s:%d headed to %s:%d\n", ipPkt.SourceAddress(), udpPkt.SourcePort(),
			ipPkt.DestinationAddress(), udpPkt.DestinationPort())
		//}
	}
}

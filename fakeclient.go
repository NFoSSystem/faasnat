package main

import (
	"fmt"
	"log"
	"net"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/header"
)

//func main() {
func Main() {
	/*payload := []byte("example payload")
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{net.IPv4(127, 0, 0, 1), 53, ""})
	if err != nil {
		log.Fatalf("Error opening UDP connection: %s\n", err)
	}
	defer conn.Close()

	size, err := conn.Write(payload)
	if err != nil {
		log.Fatalf("Error sending byte buffer %v via UDP: %s\n", payload, err)
	}

	log.Printf("Content of payload written to socket: %v\n", payload[:size])

	buff := make([]byte, 65535)
	size, err = conn.Read(buff)
	if err != nil {
		log.Fatalf("Error reading from UDP socket after write: %s\n", err)
	}

	log.Printf("Content of buffer after read: %v\n", buff[:size])*/

	// conn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(127, 0, 0, 1), ""})
	// if err != nil {
	// 	log.Fatalf("Error opening UDP socket on port 53: %s\n", err)
	// }
	// defer conn.Close()

	// for {
	// 	buff := make([]byte, 65535)
	// 	size, err := conn.Read(buff)
	// 	if err != nil {
	// 		log.Fatalf("Error reading from UDP port 53: %s\n", err)
	// 		continue
	// 	}

	// 	ipPkt := header.IPv4(buff[:size])
	// 	//addr := ipPkt.SourceAddress()
	// 	//log.Printf("SourceAddress of incoming packet: %s\n", addr.String())
	// 	udpPkt := header.UDP(ipPkt.Payload())
	// 	//log.Printf("DestinationPort of incoming UDP packet: %d\n", udpPkt.DestinationPort())
	// 	if udpPkt.DestinationPort() != uint16(53) {
	// 		continue
	// 	}

	// 	printIPDatagramContent(&ipPkt, uint16(size))
	// 	printUDPPacketContent(&udpPkt, uint16(udpPkt.Length()))
	// }

	var pkt []byte = []byte{0x45, 0x0, 0x0, 0x21, 0xc2, 0xf8, 0x40, 0x0, 0x40, 0x11, 0x79,
		0xd1, 0x7f, 0x0, 0x0, 0x1, 0x7f, 0x0, 0x0, 0x1, 0x94, 0xd9, 0x0, 0x35, 0x0, 0xd,
		0xfe, 0x20, 0x74, 0x65, 0x73, 0x74, 0xa}

	ipPkt := header.IPv4(pkt)
	ipPkt.SetSourceAddress(tcpip.Address([]byte{192, 168, 1, 248}))
	ipPkt.SetDestinationAddress(tcpip.Address([]byte{8, 8, 8, 8}))
	chksm := ipPkt.CalculateChecksum()
	ipPkt.SetChecksum(chksm)

	srcPort := header.UDP(ipPkt.Payload()).SourcePort()

	pkt = []byte(ipPkt)

	cConn, err := net.DialUDP("udp", nil, &net.UDPAddr{net.IPv4(127, 0, 0, 1), 5000, ""})
	if err != nil {
		log.Fatalf("Error opening UDP socket on port 53: %s\n", err)
	}
	defer cConn.Close()

	sConn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(127, 0, 0, 1), ""})
	if err != nil {
		log.Fatalf("Error opening UDP socket on port %d: %s\n", srcPort, err)
	}
	defer sConn.Close()

	for {
		cConn.Write(pkt)
		log.Printf("Packet sent to %s:53\n", ipPkt.DestinationAddress())

	inner:
		buff := make([]byte, 65535)
		sConn.Read(buff)
		rIpPkt := header.IPv4(buff)
		rUdpPkt := header.UDP(rIpPkt.Payload())
		if rUdpPkt.DestinationPort() != srcPort {
			goto inner
		}
		log.Printf("Payload of packet received from %s:%d -> %v\n", rIpPkt.SourceAddress(), rUdpPkt.SourcePort(),
			rUdpPkt.Payload())
	}
}

func printIPDatagramContent(pkt *header.IPv4, size uint16) {
	dtrContent := []byte(*pkt)

	fmt.Printf("Dump of received packet: %v\n\tSourceAddress: %s\n\tDestinationAddress: %s\n\tPayload: %v\n\tPayloadLength: %v\n\tTotalLength: %d\n\tChecksum: %d\n",
		dtrContent[:size], pkt.SourceAddress(), pkt.DestinationAddress(), pkt.Payload(), pkt.PayloadLength(), pkt.TotalLength(),
		pkt.Checksum())
}

func printUDPPacketContent(pkt *header.UDP, size uint16) {
	pktContent := []byte(*pkt)

	fmt.Printf("Dump of received packet: %v\n\tSourcePort: %d\n\tDestinationPort: %d\n\tPayload: %v\n\tLength: %d\n\tChecksum: %d\n",
		pktContent[:size], pkt.SourcePort(), pkt.DestinationPort(), pkt.Payload(), pkt.Length(), pkt.Checksum())
}

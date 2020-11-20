package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/header"
)

var msgPayload []byte = []byte{0x45, 0x0, 0x0, 0x21, 0xc2, 0xf8, 0x40, 0x0, 0x40, 0x11, 0x79,
	0xd1, 0x7f, 0x0, 0x0, 0x1, 0x7f, 0x0, 0x0, 0x1, 0x94, 0xd9, 0x0, 0x35, 0x0, 0xd,
	0xfe, 0x20, 0x74, 0x65, 0x73, 0x74, 0xa}

//func main() {
func main() {
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < 50; i++ {
		lastIpByte := byte(rand.Intn(253-101) + 101)
		srcPort := uint16(rand.Intn(65535-1023) + 1023)
		dstPort := uint16(rand.Intn(65535-1023) + 1023)
		client(tcpip.Address([]byte{192, 168, 1, lastIpByte}), tcpip.Address([]byte{8, 8, 8, 8}), srcPort, dstPort)
	}
}

func client(srcAddr, dstAddr tcpip.Address, srcPort, dstPort uint16) {
	ipPkt := header.IPv4(msgPayload)
	ipPkt.SetSourceAddress(srcAddr)
	ipPkt.SetDestinationAddress(dstAddr)
	udpPkt := header.UDP(ipPkt.Payload())
	udpPkt.SetSourcePort(srcPort)
	udpPkt.SetDestinationPort(dstPort)

	// Calculate UDP Packet checksum
	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, ipPkt.SourceAddress(), ipPkt.DestinationAddress(),
		uint16(len(udpPkt)))
	xsum = header.Checksum(udpPkt.Payload(), xsum)
	udpPkt.SetChecksum(^udpPkt.CalculateChecksum(xsum))

	// Calculate IP Datagram checksum
	chksm := ipPkt.CalculateChecksum()
	ipPkt.SetChecksum(chksm)

	payload := ipPkt.Payload()
	copy(payload, []byte(udpPkt))

	tmpPayload := []byte(ipPkt)

	cConn, err := net.DialUDP("udp", nil, &net.UDPAddr{net.IPv4(127, 0, 0, 1), 5000, ""})
	if err != nil {
		log.Fatalf("Error opening UDP socket on port 53: %s\n", err)
	}
	defer cConn.Close()

	cConn.Write(tmpPayload)
	log.Printf("Packet set from %s:%d to %s:%d\n", srcAddr, srcPort, dstAddr, dstPort)
}

func server(srcPort uint16) {
	sConn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(127, 0, 0, 1), ""})
	if err != nil {
		log.Fatalf("Error opening UDP socket on port %d: %s\n", srcPort, err)
	}
	defer sConn.Close()

	for {
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

func clientServer(srcAddr, dstAddr tcpip.Address, srcPort, dstPort uint16) {
	ipPkt := header.IPv4(msgPayload)
	ipPkt.SetSourceAddress(srcAddr)
	ipPkt.SetDestinationAddress(dstAddr)
	udpPkt := header.UDP(ipPkt.Payload())
	udpPkt.SetSourcePort(srcPort)
	udpPkt.SetDestinationPort(dstPort)

	// Calculate UDP Packet checksum
	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, ipPkt.SourceAddress(), ipPkt.DestinationAddress(),
		uint16(len(udpPkt)))
	xsum = header.Checksum(udpPkt.Payload(), xsum)
	udpPkt.SetChecksum(^udpPkt.CalculateChecksum(xsum))

	// Calculate IP Datagram checksum
	chksm := ipPkt.CalculateChecksum()
	ipPkt.SetChecksum(chksm)

	payload := ipPkt.Payload()
	copy(payload, []byte(udpPkt))

	tmpPayload := []byte(ipPkt)

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
		cConn.Write(tmpPayload)
		log.Printf("Packet set from %s:%d to %s:%d\n", srcAddr, srcPort, dstAddr, dstPort)

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

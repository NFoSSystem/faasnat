package main

import (
	"log"
	"net"
	"os"
	"strconv"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/header"
)

func main() {
	args := os.Args
	if len(args) < 6 {
		log.Fatalln("Too few parameters: one paramaeter necessary - pingpong [ping|pong] srcAddr srcPort trgAddr trgPort")
	}

	srcAddr := net.ParseIP(args[2])
	tmp, _ := strconv.Atoi(args[3])
	srcPort := uint16(tmp)
	trgAddr := net.ParseIP(args[4])
	tmp, _ = strconv.Atoi(args[5])
	trgPort := uint16(tmp)

	// if args[1] == "ping" {
	// 	clientServer(srcAddr, trgAddr, srcPort, trgPort, true)
	// } else {
	// 	clientServer(srcAddr, trgAddr, srcPort, trgPort, false)
	// }

	client(srcAddr, trgAddr, srcPort, trgPort)
}

func IP2Address(addr net.IP) tcpip.Address {
	ip := []byte(addr)
	return tcpip.Address([]byte{ip[12], ip[13], ip[14], ip[15]})
}

var msgPayload []byte = []byte{0x45, 0x0, 0x0, 0x21, 0xc2, 0xf8, 0x40, 0x0, 0x40, 0x11, 0x79,
	0xd1, 0x7f, 0x0, 0x0, 0x1, 0x7f, 0x0, 0x0, 0x1, 0x94, 0xd9, 0x0, 0x35, 0x0, 0xd,
	0xfe, 0x20, 0x74, 0x65, 0x73, 0x74, 0xa}

func clientServer(srcAddr, dstAddr net.IP, srcPort, dstPort uint16, ping bool) {
	ipPkt := header.IPv4(msgPayload)
	ipPkt.SetSourceAddress(IP2Address(srcAddr))
	ipPkt.SetDestinationAddress(IP2Address(dstAddr))
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

	if ping {
		sConn, err := net.ListenUDP("udp", &net.UDPAddr{net.IPv4(0, 0, 0, 0), int(srcPort), ""})
		if err != nil {
			log.Fatalf("Error opening UDP socket on port %d: %s\n", srcPort, err)
		}
		defer sConn.Close()

		cConn, err := net.DialUDP("udp", nil, &net.UDPAddr{net.IPv4(192, 168, 1, 160), 5000, ""})
		if err != nil {
			log.Fatalf("Error opening UDP socket on port 5000: %s\n", err)
		}
		defer cConn.Close()

		for {
			cConn.Write(tmpPayload)
			log.Printf("Packet sent from %s:%d to %s:%d\n", srcAddr, srcPort, dstAddr, dstPort)

			//inner1:
			buff := make([]byte, 65535)

			_, err := sConn.Read(buff)
			if err != nil {
				log.Fatalf("Error reading packet from socket: %s", err)
			}

			rIpPkt := header.IPv4(buff)
			rUdpPkt := header.UDP(rIpPkt.Payload())

			// log.Printf("DEBUG destination port %d - srcPort %d\n", rUdpPkt.DestinationPort(), srcPort)

			// if rUdpPkt.DestinationPort() != srcPort {
			// 	goto inner1
			// }
			log.Printf("Packet received from %s:%d headed to %s:%v\n", rIpPkt.SourceAddress(), rUdpPkt.SourcePort(),
				rIpPkt.DestinationAddress(), rUdpPkt.DestinationPort())
		}
	} else {
		sConn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(0, 0, 0, 0), ""})
		if err != nil {
			log.Fatalf("Error opening UDP socket on port %d: %s\n", srcPort, err)
		}
		defer sConn.Close()

		cConn, err := net.DialUDP("udp", &net.UDPAddr{srcAddr, int(srcPort), ""}, &net.UDPAddr{dstAddr, int(dstPort), ""})
		if err != nil {
			log.Fatalf("Error opening UDP socket on port 53: %s\n", err)
		}
		defer cConn.Close()

		var rUdpPkt header.UDP
		for {
		inner2:
			buff := make([]byte, 65535)
			sConn.Read(buff)
			rIpPkt := header.IPv4(buff)
			rUdpPkt = header.UDP(rIpPkt.Payload())
			if rUdpPkt.DestinationPort() != srcPort {
				goto inner2
			}
			log.Printf("Packet received from %s:%d headed to %s:%v\n", rIpPkt.SourceAddress(), rUdpPkt.SourcePort(),
				rIpPkt.DestinationAddress(), rUdpPkt.DestinationPort())

			cConn.Write(rUdpPkt.Payload())
			log.Printf("Packet sent from %s:%d to %s:%d\n", srcAddr, srcPort, dstAddr, dstPort)
		}
	}
}

func client(srcAddr, dstAddr net.IP, srcPort, dstPort uint16) {
	ipPkt := header.IPv4(msgPayload)
	ipPkt.SetSourceAddress(IP2Address(srcAddr))
	ipPkt.SetDestinationAddress(IP2Address(dstAddr))
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

	natAddr := net.IPv4(192, 168, 1, 160)
	natPort := 5000

	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{natAddr, natPort, ""})
	if err != nil {
		log.Fatalf("Error opening UDP socket to %s:%d\n", natAddr, natPort)
	}

	//t := time.NewTicker(time.Millisecond)
	cnt := 1
	for {
		_, err := conn.Write(tmpPayload)
		if err != nil {
			log.Printf("Error sending packet via UDP socket: %s\n", err)
		}
		go log.Printf("Packet %d sent to %s:%d\n", cnt, dstAddr, dstPort)
		cnt++
		//<-t.C
	}
}

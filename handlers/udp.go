package handlers

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/header"
	"github.com/howeyc/crc16"
	"github.com/willf/bitset"
)

const (
	UDP_CONNECTION_TIMEOUT_IN_SECONDS = 120
	ROUTE_FILENAME                    = "/proc/net/route"
	GATEWAY_LINE                      = 2    // line containing the gateway addr. (first line: 0)
	SEP                               = "\t" // field separator
	FIELD                             = 2
)

type tuple struct {
	addr net.IP
	port uint16
}

type tupleCouple struct {
	in  *tuple
	out *tuple
}

type packet []byte

type mappingVal struct {
	port        uint16
	flow        chan packet
	stop        chan struct{}
	lastUseTime *time.Time
}

func getCrc16FromIPAndPort(addr *net.IP, port uint16) uint16 {
	tSlice := []byte(*addr)
	tSlice = append(tSlice, byte(port>>8))
	tSlice = append(tSlice, byte(port|255))
	return crc16.ChecksumIBM(tSlice)
}

type udpMapping struct {
	i2oMap map[uint16]mappingVal
	o2iMap map[uint16]mappingVal
	mv2tc  map[*mappingVal]*tupleCouple
	mu     sync.Mutex
	bSet   *bitset.BitSet
}

func getGatewayIP() net.IP {
	file, err := os.Open(ROUTE_FILENAME)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for i := 0; i < GATEWAY_LINE; i++ {
		scanner.Scan()
	}

	tokens := strings.Split(scanner.Text(), SEP)
	gatewayHex := "0x" + tokens[FIELD]

	d, _ := strconv.ParseInt(gatewayHex, 0, 64)
	d32 := uint32(d)
	ipd32 := make(net.IP, 4)
	binary.LittleEndian.PutUint32(ipd32, d32)
	return net.IP(ipd32)
}

func getLocalIpAddr() (*net.IP, error) {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		fmt.Printf("Error obtaining the gateway address")
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil &&
			!ipnet.IP.IsLoopback() {
			return &ipnet.IP, nil
		}
	}

	return nil, nil
}

func ipv4ToInt32(ip *net.IP) uint32 {
	ipB := []byte(*ip)
	return uint32(ipB[12]<<24) | uint32(ipB[13]<<16) | uint32(ipB[13]<<8) | uint32(ipB[13])
}

func getSameSubnetFunction(lIpAddr, netmaskAddr *net.IP) func(addr *net.IP) bool {
	netmask := ipv4ToInt32(netmaskAddr)
	res := ipv4ToInt32(lIpAddr) & netmask

	return func(addr *net.IP) bool {
		addrInt32 := ipv4ToInt32(addr)
		return (addrInt32 & netmask) == res
	}
}

func private2PublicChangeFields(ipv4 *header.IPv4, udp *header.UDP, port uint16) *header.IPv4 {
	ipv4.SetSourceAddress(tcpip.Address(lIpStr))
	partialCksm := ipv4.CalculateChecksum()
	udp.SetSourcePort(port)
	cksm := udp.CalculateChecksum(partialCksm)
	udp.SetChecksum(cksm)
	payload := ipv4.Payload()
	copy(payload, []byte(*udp))
	return ipv4
}

func public2PrivateChangeFields(ipv4 *header.IPv4, udp *header.UDP, dstAddr *net.IP, port uint16) *header.IPv4 {
	ipv4.SetDestinationAddress(tcpip.Address(dstAddr.String()))
	partialCksm := ipv4.CalculateChecksum()
	udp.SetDestinationPort(port)
	cksm := udp.CalculateChecksum(partialCksm)
	udp.SetChecksum(cksm)
	payload := ipv4.Payload()
	copy(payload, []byte(*udp))
	return ipv4
}

var lIp *net.IP
var lIpInt32 uint32
var lIpStr string
var isInSameSubnet func(*net.IP) bool

func init() {
	lIp, err := getLocalIpAddr()
	if err != nil {
		log.Printf("Error obtaining local IP address: %s\n", err)
		return
	}

	lIpInt32 = ipv4ToInt32(lIp)
	lIpStr = lIp.String()
	netmask := net.IPv4(255, 255, 255, 0)
	isInSameSubnet = getSameSubnetFunction(lIp, &netmask)

	log.Printf("Local IP address resolved to: %s\n", lIpStr)
	log.Printf("Netmask resolved to: %s\n", netmask)
}

func StartUDPNat() {
	um := new(udpMapping)
	um.i2oMap = make(map[uint16]mappingVal)
	um.o2iMap = make(map[uint16]mappingVal)
	um.mv2tc = make(map[*mappingVal]*tupleCouple)
	um.bSet = bitset.New(65535)

	//conn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(0, 0, 0, 0), ""})
	conn, err := net.ListenUDP("udp", &net.UDPAddr{net.IPv4(127, 0, 0, 1), 5000, ""})
	if err != nil {
		log.Printf("Error instantiating UDP NAT: %s\n", err)
		return
	}

	for {
		buff := make([]byte, 65535)
		size, err := conn.Read(buff)
		if err != nil {
			log.Printf("Error reading from UDP socket: %s\n", err)
			continue
		}

		ipv4 := header.IPv4(buff[:size])
		log.Printf("Packet received from %s headed to %s\n", ipv4.SourceAddress(), ipv4.DestinationAddress())

		go um.handlePacket(buff[:size])
	}

}

func (mv *mappingVal) sendPkt(dstAddr *net.IP, dstPort uint16) {
	log.Printf("Opening socket for %s:%d\n", dstAddr.String(), dstPort)
	conn, err := net.Dial("ip4:udp", dstAddr.String())
	if err != nil {
		log.Printf("Error opening UDP connection to host %s: %s\n", dstAddr, err)
		return
	}
	defer conn.Close()

	timer := time.NewTimer(UDP_CONNECTION_TIMEOUT_IN_SECONDS * time.Second)

	for {
		select {
		case pkt := <-mv.flow:
			log.Printf("---------------> Packet sent to %s:%d\n", dstAddr.String(), dstPort)
			conn.Write(pkt)
			tmp := time.Now()
			mv.lastUseTime = &tmp
			continue
		case <-mv.stop:
			return
		case <-timer.C:
			if deltaTime := time.Since(*mv.lastUseTime) * time.Second; deltaTime <= UDP_CONNECTION_TIMEOUT_IN_SECONDS {
				timer = time.NewTimer((UDP_CONNECTION_TIMEOUT_IN_SECONDS - deltaTime) * time.Second)
				continue
			} else {
				return
			}
		}
	}
}

func (um *udpMapping) getOuterFlowChan(it, ot tuple) chan packet {
	um.mu.Lock()

	if val, ok := um.i2oMap[getCrc16FromIPAndPort(&it.addr, it.port)]; !ok {
		mv := mappingVal{}
		mv.flow = make(chan packet)
		mv.stop = make(chan struct{})
		tc := new(tupleCouple)
		tc.in = &it
		tc.out = &ot
		tmp, err := um.getFirstAvailablePort(&it)
		if err != nil {
			log.Println("ERROR all available UDP ports are used!")
			return nil
		}
		mv.port = tmp
		um.i2oMap[getCrc16FromIPAndPort(&it.addr, it.port)] = mv

		addrSlice := make([]byte, len([]byte(it.addr))+len([]byte(ot.addr)))
		addrSlice = append(addrSlice, []byte(it.addr)...)
		addrSlice = append(addrSlice, []byte(ot.addr)...)
		cks := crc16.ChecksumIBM(addrSlice)
		um.o2iMap[cks] = mv
		um.mv2tc[&mv] = tc
		um.mu.Unlock()
		go mv.sendPkt(&it.addr, it.port)
		return mv.flow
	} else {
		tmp := time.Now()
		val.lastUseTime = &tmp
		um.mu.Unlock()
		return val.flow
	}
}

func (um *udpMapping) getInnerFlowChan(ot, it tuple) chan packet {
	um.mu.Lock()

	addrSlice := make([]byte, len([]byte(it.addr))+len([]byte(ot.addr)))
	addrSlice = append(addrSlice, []byte(it.addr)...)
	addrSlice = append(addrSlice, []byte(ot.addr)...)
	cks := crc16.ChecksumIBM(addrSlice)

	if val, ok := um.o2iMap[cks]; !ok {
		// no flow has been opened, the external network incoming packet is filtered
		um.mu.Unlock()
		return nil
	} else {
		// a flow is open, the lastUseTime attribute is updated and the flow is returned
		um.mu.Lock()
		tmp := time.Now()
		val.lastUseTime = &tmp
		um.mu.Unlock()
		return val.flow
	}
}

func isEven(num uint16) bool {
	return num%2 == 0
}

func (um *udpMapping) getFirstAvailablePort(t *tuple) (uint16, error) {
	var lower, upper uint16
	if t.port < 1024 {
		lower, upper = 0, 1024
	} else {
		lower, upper = 1024, 65535
	}

	var even bool = t.port%2 == 0

	for i := uint16(lower); i < upper; i++ {
		if (!even || isEven(i)) && !um.bSet.Test(uint(i)) {
			return i, nil
		}
	}

	return 0, nil
}

func getTupleFromUDPPacket(ipv4 header.IPv4, udp header.UDP) (*tupleCouple, error) {
	s := new(tuple)
	d := new(tuple)

	rawIPPkt := []byte(ipv4)
	srcAddr := net.IPv4(rawIPPkt[12], rawIPPkt[13], rawIPPkt[14], rawIPPkt[15])
	dstAddr := net.IPv4(rawIPPkt[16], rawIPPkt[17], rawIPPkt[18], rawIPPkt[19])

	s.addr = srcAddr
	s.port = udp.SourcePort()

	d.addr = dstAddr
	d.port = udp.DestinationPort()

	tc := new(tupleCouple)
	tc.in, tc.out = s, d

	return tc, nil
}

func (um *udpMapping) handlePacket(ipPkt packet) error {
	pkt := header.IPv4(ipPkt)
	if pkt == nil || len(pkt) == 0 || pkt.Payload() == nil || len(pkt.Payload()) == 0 {
		return fmt.Errorf("Error packet provided in input is null or empty!")
	}

	tc, err := getTupleFromUDPPacket(pkt, header.UDP(pkt.Payload()))
	if err != nil {
		return err
	}

	ipAddr := net.IPv4(ipPkt[12], ipPkt[13], ipPkt[14], ipPkt[15])

	if !isInSameSubnet(&ipAddr) {
		return nil
	}

	log.Printf("Packet received from %s:%d headed to %s:%d\n", tc.in.addr, tc.in.port, tc.out.addr, tc.out.port)

	var pktChan chan packet

	if isInSameSubnet(&ipAddr) {
		pktChan = um.getOuterFlowChan(*tc.in, *tc.out)
		if pktChan == nil {
			return nil
		}
		//log.Println("------------> Packet sent to outer flow")
		pktChan <- packet(pkt)
	} else {
		//log.Printf("Address %s and %s don't belong to the same subnet!\n", lIpStr, tc.in.addr)
		pktChan = um.getInnerFlowChan(*tc.out, *tc.in)
		if pktChan == nil {
			return nil
		}
		//log.Println("------------> Packet sent to inner flow")
		pktChan <- packet(pkt)
	}

	return nil
}

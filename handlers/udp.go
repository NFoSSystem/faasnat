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
	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/raw"
	"github.com/willf/bitset"
)

const (
	UDP_CONNECTION_TIMEOUT_IN_SECONDS = 185
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
	mu     sync.RWMutex
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
	return uint32(ipB[12])<<24 | uint32(ipB[13])<<16 | uint32(ipB[14])<<8 | uint32(ipB[15])
}

func getSameSubnetFunction(lIpAddr, netmaskAddr *net.IP) func(addr *net.IP) bool {
	netmask := ipv4ToInt32(netmaskAddr)
	res := ipv4ToInt32(lIpAddr) & netmask

	return func(addr *net.IP) bool {
		addrInt32 := ipv4ToInt32(addr)
		return (addrInt32 & netmask) == res
	}
}

func isEqualToIp(ipAddr *net.IP) func(*net.IP) bool {
	src := ipv4ToInt32(ipAddr)

	return func(dest *net.IP) bool {
		return src == ipv4ToInt32(dest)
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
var isEqualToSrcIp func(*net.IP) bool
var isEqualToSrcIp2 func(*net.IP) bool

func init() {
	lIp, err := getLocalIpAddr()
	if err != nil {
		log.Printf("Error obtaining local IP address: %s\n", err)
		return
	}

	lIpInt32 = ipv4ToInt32(lIp)
	lIpStr = lIp.String()
	netmask := net.IPv4(255, 255, 255, 0)
	iIp := net.IPv4(10, 10, 1, 2)
	iIp2 := net.IPv4(10, 10, 2, 1)
	isInSameSubnet = getSameSubnetFunction(&iIp, &netmask)
	isEqualToSrcIp = isEqualToIp(&iIp)
	isEqualToSrcIp2 = isEqualToIp(&iIp2)

	log.Printf("Local IP address resolved to: %s\n", lIpStr)
	log.Printf("Netmask resolved to: %s\n", netmask)
}

func StartUDPNat(interfaces []string) {
	stopChan := make(chan struct{})
	um := new(udpMapping)
	um.i2oMap = make(map[uint16]mappingVal)
	um.o2iMap = make(map[uint16]mappingVal)
	um.mv2tc = make(map[*mappingVal]*tupleCouple)
	um.bSet = bitset.New(65535)

	for _, nf := range interfaces {
		go startUDPNatHelper(nf, um)
	}
	<-stopChan
}

func startUDPNatHelper(interfaceName string, um *udpMapping) {
	nif, err := net.InterfaceByName(interfaceName)
	if err != nil {
		log.Fatalf("Error accessing to interface %s\n", interfaceName)
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

		ipBuff := []byte(eFrame.Payload)

		switch ipBuff[9] {
		case 17:
			// UDP
			go um.handlePacket(ipBuff)
		default:
			continue
		}
	}

}

func (mv *mappingVal) sendPkt(dstAddr *net.IP, dstPort uint16) {
	log.Printf("Opening socket for %s:%d - Start time: %d\n", dstAddr.String(), dstPort, time.Now().UnixNano())
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{*dstAddr, int(dstPort), ""})
	if err != nil {
		log.Printf("Error opening UDP connection to host %s: %s\n", dstAddr, err)
		return
	}
	defer conn.Close()

	timer := time.NewTimer(UDP_CONNECTION_TIMEOUT_IN_SECONDS * time.Second)

	for {
		select {
		case pkt := <-mv.flow:
			_, err = conn.Write(pkt)
			if err != nil {
				log.Printf("Error sending packet to %s:%d: %s\n", dstAddr.String(), dstPort, err)
			}
			tmp := time.Now()
			mv.lastUseTime = &tmp
			continue
		case <-mv.stop:
			return
		case <-timer.C:
			if deltaTime := time.Since(*mv.lastUseTime) / time.Second; deltaTime <= UDP_CONNECTION_TIMEOUT_IN_SECONDS {
				timer = time.NewTimer((UDP_CONNECTION_TIMEOUT_IN_SECONDS - deltaTime) * time.Second)
				continue
			} else {
				return
			}
		}
	}
}

func (um *udpMapping) getOuterFlowChan(it, ot tuple) chan packet {
	crcRes := getCrc16FromIPAndPort(&it.addr, it.port)
	um.mu.RLock()
	if val, ok := um.i2oMap[crcRes]; !ok {
		um.mu.RUnlock()
		mv := mappingVal{}
		mv.flow = make(chan packet)
		mv.stop = make(chan struct{})
		tc := new(tupleCouple)
		tc.in = &it
		tc.out = &ot
		um.mu.Lock()
		tmp, err := um.getFirstAvailablePort(&it)
		if err != nil {
			log.Println("ERROR all available UDP ports are used!")
			return nil
		}
		mv.port = tmp
		um.i2oMap[crcRes] = mv

		addrSlice := make([]byte, len([]byte(it.addr))+len([]byte(ot.addr)))
		addrSlice = append(addrSlice, []byte(it.addr)...)
		addrSlice = append(addrSlice, []byte(ot.addr)...)
		cks := crc16.ChecksumIBM(addrSlice)
		um.o2iMap[cks] = mv
		um.mv2tc[&mv] = tc
		um.mu.Unlock()
		go mv.sendPkt(&ot.addr, ot.port)
		return mv.flow
	} else {
		tmp := time.Now()
		val.lastUseTime = &tmp
		um.mu.RUnlock()
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

	udpPkt := header.UDP(pkt.Payload())
	tc, err := getTupleFromUDPPacket(pkt, udpPkt)
	if err != nil {
		return err
	}

	ipAddr := net.IPv4(ipPkt[12], ipPkt[13], ipPkt[14], ipPkt[15])

	//log.Printf("Packet received from %s:%d headed to %s:%d\n", tc.in.addr, tc.in.port, tc.out.addr, tc.out.port)

	var pktChan chan packet

	if isEqualToSrcIp(&ipAddr) || isEqualToSrcIp2(&ipAddr) {
		pktChan = um.getOuterFlowChan(*tc.in, *tc.out)
		if pktChan == nil {
			return nil
		}
		pktChan <- packet(udpPkt.Payload())
	} else {
		pktChan = um.getInnerFlowChan(*tc.out, *tc.in)
		if pktChan == nil {
			return nil
		}
		pktChan <- packet(udpPkt.Payload())
	}

	return nil
}

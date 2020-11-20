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

	"faasnat/context"

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

type packet struct {
	pktBuff []byte
	srcAddr net.IP
	srcPort uint16
	trgAddr net.IP
	trgPort uint16
}

type mappingVal struct {
	port        uint16
	flow        chan packet
	stop        chan struct{}
	lastUseTime *time.Time
}

type chanMapVal struct {
	lastUse *time.Time
	conn    *net.IPConn
}

type chanMap struct {
	im map[uint16]*chanMapVal
	mu sync.Mutex
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

func changeFieldsInPacket(pkt packet) []byte {
	ipv4 := header.IPv4(pkt.pktBuff)
	udp := header.UDP(ipv4.Payload())

	udp.SetDestinationPort(pkt.trgPort)
	udp.SetSourcePort(pkt.srcPort)

	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, ipv4.SourceAddress(), ipv4.DestinationAddress(),
		uint16(len(udp)))
	xsum = header.Checksum(udp.Payload(), xsum)
	udp.SetChecksum(^udp.CalculateChecksum(xsum))

	return []byte(udp)
}

var lIp *net.IP
var lIpInt32 uint32
var lIpStr string
var isInSameSubnet func(*net.IP) bool
var ctx *context.Context
var outCtx *context.Context
var inCtx *context.Context
var pktChan chan packet
var cMap *chanMap

func (cm *chanMap) GetConn(addr *net.IP) (*net.IPConn, error) {
	cm.mu.Lock()
	crc := crc16.ChecksumIBM([]byte(*addr))
	if val, ok := cm.im[crc]; ok {
		t := time.Now()
		val.lastUse = &t
		cm.mu.Unlock()
		return val.conn, nil
	} else {
		conn, err := net.DialIP("ip4:udp", &net.IPAddr{*lIp, ""}, &net.IPAddr{*addr, ""})
		if err != nil {
			cm.mu.Unlock()
			return nil, err
		}
		val := new(chanMapVal)
		val.conn = conn
		timer := time.NewTimer(UDP_CONNECTION_TIMEOUT_IN_SECONDS * time.Second)
		tmpUse := time.Now()
		val.lastUse = &tmpUse
		go func() {
			for {
				select {
				case <-timer.C:
					if time.Since(*val.lastUse) > (UDP_CONNECTION_TIMEOUT_IN_SECONDS * time.Second) {
						cm.mu.Lock()
						delete(cm.im, crc)
						conn.Close()
						cm.mu.Unlock()
						return
					}
				}
			}
		}()
		cm.mu.Unlock()
		return val.conn, nil
	}
}

func (cm *chanMap) sendPkt(pktChan chan packet) {
	for {
		select {
		case pkt := <-pktChan:
			conn, err := cm.GetConn(&pkt.trgAddr)
			if err != nil {
				log.Println(err)
				continue
			}

			buff := changeFieldsInPacket(pkt)
			conn.Write(buff)
			log.Printf("DEBUG Sent packet %s:%d to %s:%d\n", pkt.srcAddr, pkt.srcPort, pkt.trgAddr, pkt.trgPort)
			continue
		}
	}
}

func getOuterFlowChan(ctx, outCtx, inCtx *context.Context, it, ot tuple) (uint16, error) {
	inCrc := getCrc16FromIPAndPort(&it.addr, it.port)
	if err := outCtx.OutFlowLock(fmt.Sprintf("%s:%d 1", it.addr, it.port)); err != nil {
		return 0, fmt.Errorf("Error obtaining OutFlowLock: %s\n", err)
	}

	ip, port, err := outCtx.GetOutAddrPortCouple(it.port)
	if err != nil {
		return 0, fmt.Errorf("Error searching data from Redis Set: %s\n", err)
	}

	if err := outCtx.OutFlowUnLock(fmt.Sprintf("%s:%d 1", it.addr, it.port)); err != nil {
		return 0, err
	}

	if ip == nil {
		outPort, err := ctx.GetFirstAvailablePort(it.port)
		if err != nil {
			return 0, err
		}

		if err := outCtx.OutFlowLock(fmt.Sprintf("%s:%d 2", it.addr, it.port)); err != nil {
			return 0, fmt.Errorf("Error obtaining OutFlowLock: %s\n", err)
		}

		if err := outCtx.SetOutAddrPortCouple(inCrc, lIp, outPort); err != nil {
			return 0, err
		}

		if err := outCtx.OutFlowUnLock(fmt.Sprintf("%s:%d 2", it.addr, it.port)); err != nil {
			return 0, err
		}

		go func() {
			addrSlice := make([]byte, len([]byte(*lIp))+len([]byte(ot.addr)))
			addrSlice = append(addrSlice, []byte(*lIp)...)
			addrSlice = append(addrSlice, []byte(ot.addr)...)
			outCrc := crc16.ChecksumIBM(addrSlice)

			inCtx.InFlowLock("getOuterFlowChan")
			inCtx.SetInAddrPortCouple(outCrc, lIp, outPort)
			inCtx.InFlowUnLock()
		}()

		return outPort, nil
	} else {
		return port, nil
	}
}

func getInnerFlowChan(ctx, outCtx, inCtx *context.Context, ot, it tuple) (*net.IP, uint16, error) {
	addrSlice := make([]byte, len([]byte(it.addr))+len([]byte(ot.addr)))
	addrSlice = append(addrSlice, []byte(it.addr)...)
	addrSlice = append(addrSlice, []byte(ot.addr)...)
	cks := crc16.ChecksumIBM(addrSlice)

	if err := inCtx.InFlowLock("getInnerFlowChan"); err != nil {
		return nil, 0, fmt.Errorf("Error obtaining InFlowLock: %s\n", err)
	}

	ip, port, err := inCtx.GetInAddrPortCouple(cks)
	if err != nil {
		if err = inCtx.InFlowUnLock(); err != nil {
			log.Println("Error releasing InFlow lock: %s\n", err)
		}
		return nil, 0, fmt.Errorf("Error searching data from Redis Set: %s\n", err)
	}

	if err = inCtx.InFlowUnLock(); err != nil {
		log.Println("Error releasing InFlow lock: %s\n", err)
	}

	if ip == nil {
		// filter out incoming packet
		return nil, 0, nil
	} else {
		return ip, port, nil
	}
}

func isEven(num uint16) bool {
	return num%2 == 0
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

func (cm *chanMap) InitSenders(num uint8, pktChan chan packet) {
	for i := uint8(0); i < num; i++ {
		go cm.sendPkt(pktChan)
		log.Printf("Sender %d instantiated\n", i)
	}
}

func handlePacket(ctx, outCtx, inCtx *context.Context, ipPkt []byte, pktChan chan packet) error {
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

	if isInSameSubnet(&ipAddr) {
		port, err := getOuterFlowChan(ctx, outCtx, inCtx, *tc.in, *tc.out)
		if err != nil {
			log.Println(err)
			return nil
		}

		pktChan <- packet{pktBuff: pkt, srcAddr: *lIp, srcPort: port, trgAddr: tc.out.addr, trgPort: tc.out.port}
		log.Println("------------> Packet sent to outer flow")
	} else {
		//log.Printf("Address %s and %s don't belong to the same subnet!\n", lIpStr, tc.in.addr)
		ip, port, err := getInnerFlowChan(ctx, outCtx, inCtx, *tc.out, *tc.in)
		if err != nil {
			log.Println(err)
			return nil
		}

		pktChan <- packet{pktBuff: pkt, srcAddr: *lIp, srcPort: tc.in.port, trgAddr: *ip, trgPort: port}
		log.Println("------------> Packet sent to inner flow")
	}

	return nil
}

func init() {
	tmpIp, err := getLocalIpAddr()
	if err != nil {
		log.Printf("Error obtaining local IP address: %s\n", err)
		return
	}
	lIp = tmpIp

	lIpInt32 = ipv4ToInt32(lIp)
	lIpStr = lIp.String()
	netmask := net.IPv4(255, 255, 255, 0)
	isInSameSubnet = getSameSubnetFunction(lIp, &netmask)
	ctx, err = context.New("localhost", 6379)
	if err != nil {
		log.Fatalf("Error obtaining Context: %s\n", err)
	}

	outCtx, err = context.New("localhost", 6379)
	if err != nil {
		log.Fatalf("Error obtaining Context: %s\n", err)
	}

	inCtx, err = context.New("localhost", 6379)
	if err != nil {
		log.Fatalf("Error obtaining Context: %s\n", err)
	}

	log.Printf("Local IP address resolved to: %s\n", lIpStr)
	log.Printf("Netmask resolved to: %s\n", netmask)

	pktChan = make(chan packet, 100)
	cMap = new(chanMap)
	cMap.InitSenders(10, pktChan)
	ctx.CleanUpSets()
	ctx.InitPortsBitSet()
	outCtx.CleanUpSets()
	outCtx.InitPortsBitSet()
	inCtx.CleanUpSets()
	inCtx.InitPortsBitSet()

	log.Printf("DEBUG lIp: %s\n", *lIp)
}

func StartUDPNat() {
	//conn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(0, 0, 0, 0), ""})
	conn, err := net.ListenUDP("udp", &net.UDPAddr{net.IPv4(127, 0, 0, 1), 5000, ""})
	if err != nil {
		log.Printf("Error instantiating UDP NAT: %s\n", err)
		return
	}
	defer conn.Close()
	defer ctx.Close()

	for {
		buff := make([]byte, 65535)
		size, err := conn.Read(buff)
		if err != nil {
			log.Printf("Error reading from UDP socket: %s\n", err)
			continue
		}

		ipv4 := header.IPv4(buff[:size])
		log.Printf("Packet received from %s headed to %s\n", ipv4.SourceAddress(), ipv4.DestinationAddress())

		log.Printf("DEBUG lIp: %s\n", *lIp)

		go handlePacket(ctx, inCtx, outCtx, buff[:size], pktChan)
	}

}

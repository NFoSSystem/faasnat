package handlers

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"nflib"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"natdb"
	"utils"

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
	conn    *net.UDPConn
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
		utils.Log.Fatal(err)
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
	//return net.ParseIP("172.17.0.3")
}

func getLocalIpAddr() (*net.IP, error) {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		utils.Log.Printf("Error obtaining the gateway address\n")
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
	return uint32(ipB[12]<<24) | uint32(ipB[13]<<16) | uint32(ipB[14]<<8) | uint32(ipB[15])
}

func getSameSubnetFunction(lIpAddr, netmaskAddr *net.IP) func(addr *net.IP) bool {
	netmask := ipv4ToInt32(netmaskAddr)
	res := ipv4ToInt32(lIpAddr) & netmask

	return func(addr *net.IP) bool {
		addrInt32 := ipv4ToInt32(addr)
		return (addrInt32 & netmask) == res
	}
}

func changeFieldsInPacket(pkt packet) packet {
	ipv4 := header.IPv4(pkt.pktBuff)
	udp := header.UDP(ipv4.Payload())

	udp.SetDestinationPort(pkt.trgPort)
	udp.SetSourcePort(pkt.srcPort)

	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, ipv4.SourceAddress(), ipv4.DestinationAddress(),
		uint16(len(udp)))
	xsum = header.Checksum(udp.Payload(), xsum)
	udp.SetChecksum(^udp.CalculateChecksum(xsum))

	pkt.pktBuff = []byte(udp)
	return pkt
}

var lIp *net.IP
var gIp *net.IP
var rIp *net.IP
var lIpInt32 uint32
var lIpStr string
var isInSameSubnet func(*net.IP) bool
var pktChan chan packet
var cMap *chanMap
var sc *natdb.SharedContext

var PacketChan chan nflib.Packet
var GetFirstAvailablePort func(port uint16) uint16

// TO BE REMOVED --------------------------------------------------------------------------------------------
var srcIp1 net.IP = net.ParseIP("172.18.0.4")
var srcIp2 net.IP = net.ParseIP("172.18.0.6")
var srcIp1Num uint32 = ipv4ToInt32(&srcIp1)
var srcIp2Num uint32 = ipv4ToInt32(&srcIp2)

var destIp1 net.IP = net.ParseIP("10.10.2.1")
var destIp2 net.IP = net.ParseIP("172.17.0.4")

// TO BE REMOVED --------------------------------------------------------------------------------------------

func (cm *chanMap) GetConn(srcAddr, trgAddr *net.IP, srcPort, trgPort uint16) (*net.UDPConn, error) {
	cm.mu.Lock()
	crc := crc16.ChecksumIBM([]byte(*trgAddr))
	if val, ok := cm.im[crc]; ok {
		t := time.Now()
		val.lastUse = &t
		cm.mu.Unlock()
		return val.conn, nil
	} else {
		// TO BE REMOVED --------------------------------------------------------------------------------------
		// //utils.Log.Printf("DEBUG Enter here")
		// var targetAddr *net.IP
		// if trgPort == 8422 {
		// 	targetAddr = &destIp1
		// } else {
		// 	targetAddr = &destIp2
		// }

		// conn, err := net.DialUDP("udp", &net.UDPAddr{*lIp, int(srcPort), ""}, &net.UDPAddr{*targetAddr, int(trgPort), ""})

		// //utils.Log.Printf("DEBUG srcAddr: %s - targetAddr: %s - trgPort %d", *srcAddr, *targetAddr, trgPort)
		// // TO BE REMOVED --------------------------------------------------------------------------------------

		conn, err := net.DialUDP("udp", &net.UDPAddr{*lIp, int(srcPort), ""}, &net.UDPAddr{*trgAddr, int(trgPort), ""})
		if err != nil {
			cm.mu.Unlock()
			return nil, err
		}
		val := new(chanMapVal)
		val.conn = conn
		timer := time.NewTimer(UDP_CONNECTION_TIMEOUT_IN_SECONDS * time.Second)
		tmpUse := time.Now()
		val.lastUse = &tmpUse
		cm.im[crc] = val
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
					return
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
			conn, err := cm.GetConn(&pkt.srcAddr, &pkt.trgAddr, pkt.srcPort, pkt.trgPort)
			if err != nil {
				utils.Log.Println(err)
				continue
			}

			_, err = conn.Write(pkt.pktBuff)
			if err != nil {
				utils.Log.Printf("Error writing to socket headed to %s:%d: %s\n", pkt.trgAddr, pkt.trgPort, err)
			}
			continue
		}
	}
}

type FlowVal struct {
	addr *net.IP
	port uint16
}

type FlowMap struct {
	mu sync.RWMutex
	im map[uint16]*FlowVal
}

func NewFlowMap() *FlowMap {
	res := new(FlowMap)
	res.im = make(map[uint16]*FlowVal)
	return res
}

func (fm *FlowMap) Set(crc16 uint16, addr *net.IP, port uint16) {
	fm.mu.Lock()
	fm.im[crc16] = &FlowVal{addr, port}
	fm.mu.Unlock()
}

func (fm *FlowMap) Get(crc16 uint16) *FlowVal {
	fm.mu.RLock()
	if val, ok := fm.im[crc16]; !ok {
		fm.mu.RUnlock()
		return nil
	} else {
		fm.mu.RUnlock()
		return val
	}
}

func getOuterFlowChan(it, ot tuple, outFlowMap, inFlowMap *FlowMap) (uint16, error) {
	inCrc := getCrc16FromIPAndPort(&it.addr, it.port)
	val := outFlowMap.Get(inCrc)

	if val == nil {
		outPort := GetFirstAvailablePort(it.port)
		if outPort == 0 {
			return 0, fmt.Errorf("Error assigning out port: port returned 0")
		}

		outFlowMap.Set(inCrc, lIp, outPort)

		outCrc := getCrc16FromIPAndPort(rIp, outPort)
		inFlowMap.Set(outCrc, &it.addr, it.port)

		//PacketChan <- nflib.Packet{nflib.Ipv4ToInt32(lIp), inCrc, outPort}

		return outPort, nil
	} else {
		return val.port, nil
	}
}

func getInnerFlowChan(it, ot tuple, outFlowMap, inFlowMap *FlowMap) (*FlowVal, error) {
	inCrc := getCrc16FromIPAndPort(&it.addr, it.port)

	val := inFlowMap.Get(inCrc)

	//PacketChan <- nflib.Packet{nflib.Ipv4ToInt32(val.addr), inCrc, val.port}

	if val == nil {
		return nil, nil
	} else {
		return val, nil
	}
}

// func getInnerFlowChan(ot, it tuple) (*net.IP, uint16, error) {
// 	addrSlice := make([]byte, len([]byte(*lIp))+len([]byte(it.addr)))
// 	addrSlice = append(addrSlice, []byte(*lIp)...)
// 	addrSlice = append(addrSlice, []byte(it.addr)...)
// 	cks := crc16.ChecksumIBM(addrSlice)

// 	utils.Log.Printf("DEBUG cks %v\n", cks)

// 	lock, err := sc.InFlowLock(ot.port)
// 	if err != nil {
// 		return nil, 0, fmt.Errorf("Error obtaining InFlowLock: %s\n", err)
// 	}

// 	ip, port, err := sc.GetInAddrPortCouple(cks)
// 	if err != nil {
// 		err = sc.InFlowUnLock(lock)
// 		if err != nil {
// 			utils.Log.Println("Error releasing InFlow lock: %s\n", err)
// 		}
// 		return nil, 0, fmt.Errorf("Error searching data from Redis Set: %s\n", err)
// 	}

// 	if err = sc.InFlowUnLock(lock); err != nil {
// 		utils.Log.Println("Error releasing InFlow lock: %s\n", err)
// 	}

// 	if ip == nil {
// 		// filter out incoming packet
// 		return nil, 0, nil
// 	} else {
// 		return ip, port, nil
// 	}
// }

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
		//utils.Log.Printf("Sender %d instantiated\n", i)
	}
}

func handlePacket(ipPkt []byte, pktChan chan packet, outFlowMap, inFlowMap *FlowMap) error {
	pkt := header.IPv4(ipPkt)

	if pkt == nil || len(pkt) == 0 || pkt.Payload() == nil || len(pkt.Payload()) == 0 {
		utils.Log.Printf("Error packet provided in input is null or empty!")
		return nil
	}

	tc, err := getTupleFromUDPPacket(pkt, header.UDP(pkt.Payload()))
	if err != nil {
		return err
	}

	udpPkt := header.UDP(pkt.Payload())
	ipAddr := net.IPv4(ipPkt[12], ipPkt[13], ipPkt[14], ipPkt[15])

	// TO BE REMOVED - START
	// ip := net.IPv4(10, 10, 1, 1)
	// destIp := ipv4ToInt32(&ip)
	// TO BE REMOVED - END

	if isInSameSubnet(&ipAddr) /*&& destIp != ipv4ToInt32(&ipAddr)*/ {
		port, err := getOuterFlowChan(*tc.in, *tc.out, outFlowMap, inFlowMap)
		if err != nil {
			utils.Log.Println(err)
			return nil
		}

		//pktChan <- packet{pktBuff: udpPkt.Payload(), srcAddr: *gIp, srcPort: port, trgAddr: tc.out.addr, trgPort: tc.out.port}
		pktChan <- packet{pktBuff: udpPkt.Payload(), srcAddr: *gIp, srcPort: port, trgAddr: destIp1, trgPort: tc.out.port}
	} else {
		val, err := getInnerFlowChan(*tc.out, *tc.in, outFlowMap, inFlowMap)
		if err != nil {
			utils.Log.Println(err)
			return nil
		}

		if val == nil {
			//utils.Log.Println("DEBUG Exit here")
			// filter incoming packet
			return nil
		}

		// optPkt := changeFieldsInPacket(packet{pktBuff: pkt, srcAddr: tc.in.addr, srcPort: tc.in.port, trgAddr: *ip, trgPort: port})
		// pktChan <- packet(optPkt)
		pktChan <- packet{pktBuff: udpPkt.Payload(), srcAddr: tc.in.addr, srcPort: tc.in.port, trgAddr: *val.addr, trgPort: val.port}
	}

	return nil
}

func InitNat(ports []uint16) {
	tmpIp, err := getLocalIpAddr()
	if err != nil {
		utils.Log.Printf("Error obtaining local IP address: %s\n", err)
		return
	}
	lIp = tmpIp

	lIpInt32 = ipv4ToInt32(lIp)
	lIpStr = lIp.String()
	netmask := net.IPv4(255, 255, 255, 0)
	isInSameSubnet = getSameSubnetFunction(lIp, &netmask)

	tmpGIp := getGatewayIP()

	gIp = &tmpGIp

	tmpRIp := net.ParseIP("10.10.1.1")
	rIp = &tmpRIp

	// utils.Log.Printf("Local IP address resolved to: %s\n", lIpStr)
	// utils.Log.Printf("Netmask resolved to: %s\n", netmask)

	pktChan = make(chan packet, 100)
	cMap = new(chanMap)
	cMap.im = make(map[uint16]*chanMapVal)
	cMap.InitSenders(10, pktChan)
	sc = natdb.New(getGatewayIP().String(), 6379)

	var mu sync.Mutex
	var lowerPtr, upperPtr int = 0, 0
	for i := 0; i < len(ports); i++ {
		if ports[i] > 1023 {
			upperPtr = i
			break
		}
	}

	GetFirstAvailablePort = func(port uint16) uint16 {
		mu.Lock()
		var res uint16 = 0
		if port < 1024 {
			res = ports[lowerPtr]
			if len(ports) > 1 {
				ports = ports[lowerPtr+1:]
			}
			lowerPtr++
		} else {
			res = ports[upperPtr]
			if len(ports) > upperPtr {
				ports = append(ports[:upperPtr], ports[upperPtr+1:]...)
			}
			upperPtr++
		}
		mu.Unlock()
		return res
	}
}

func StartUDPNat(listenPort int, ports []uint16, outFlowMap, inFlowMap *FlowMap, cntId uint16, repl bool) {
	InitNat(ports)
	defer sc.Close()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{net.IPv4(0, 0, 0, 0), listenPort, ""})
	if err != nil {
		utils.Log.Printf("Error instantiating UDP NAT: %s\n", err)
		return
	}

	nflib.SendPingMessageToRouter("nat", utils.Log, utils.Log, cntId, repl)

	defer conn.Close()

	buff := make([]byte, 65535)

	terminateSignal := len([]byte("terminate"))

	for {
		size, err := conn.Read(buff)
		if err != nil {
			utils.Log.Printf("Error reading from UDP socket: %s\n", err)
			continue
		}

		if size == terminateSignal && string(buff[:size]) == "terminate" {
			//utils.Log.Println("Received termination message from faasrouter")
			os.Exit(0)
		}

		go handlePacket(buff[:size], pktChan, outFlowMap, inFlowMap)
	}
}

func StartIPInterface(listenPort uint16, outFlowMap, inFlowMap *FlowMap) {
	conn, err := net.ListenIP("ip4:udp", &net.IPAddr{net.IPv4(0, 0, 0, 0), ""})
	if err != nil {
		utils.Log.Printf("Error opening connection on IP interface: %s\n", err)
		return
	}

	ip := net.IPv4(192, 168, 1, 102)
	destIp := ipv4ToInt32(&ip)

	for {
		buff := make([]byte, 65535)
		size, err := conn.Read(buff)
		if err != nil {
			utils.Log.Printf("Error reading from UDP socket: %s\n", err)
			continue
		}

		ip4Pkt := header.IPv4(buff[:size])
		udpPkt := header.UDP(ip4Pkt.Payload())

		if udpPkt.DestinationPort() == listenPort {
			utils.Log.Printf("Skipping incoming packet on port %d\n", listenPort)
			continue
		}

		srcAddr := ip4Pkt.SourceAddress()
		res := ipv4ToInt32(addressToIPv4(srcAddr))
		if res != destIp {
			continue
		}

		go handlePacket(buff[:size], pktChan, outFlowMap, inFlowMap)
	}

}

func addressToIPv4(addr tcpip.Address) *net.IP {
	buff := []byte(addr)
	res := net.IPv4(buff[0], buff[1], buff[2], buff[3])
	return &res
}

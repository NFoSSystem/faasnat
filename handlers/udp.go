package handlers

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bitbucket.org/Manaphy91/faasnat/natdb"
	"bitbucket.org/Manaphy91/faasnat/utils"

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
var lIpInt32 uint32
var lIpStr string
var isInSameSubnet func(*net.IP) bool
var pktChan chan packet
var cMap *chanMap
var sc *natdb.SharedContext

func (cm *chanMap) GetConn(srcAddr, trgAddr *net.IP, srcPort, trgPort uint16) (*net.UDPConn, error) {
	cm.mu.Lock()
	crc := crc16.ChecksumIBM([]byte(*trgAddr))
	if val, ok := cm.im[crc]; ok {
		t := time.Now()
		val.lastUse = &t
		cm.mu.Unlock()
		return val.conn, nil
	} else {
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
			utils.Log.Printf("Sent packet %s:%d to %s:%d\n", pkt.srcAddr, pkt.srcPort, pkt.trgAddr, pkt.trgPort)
			continue
		}
	}
}

func getOuterFlowChan(it, ot tuple) (uint16, error) {
	inCrc := getCrc16FromIPAndPort(&it.addr, it.port)
	lock, err := sc.OutFlowLock(it.port)
	if err != nil {
		return 0, fmt.Errorf("Error obtaining OutFlowLock: %s\n", err)
	}

	ip, port, err := sc.GetOutAddrPortCouple(inCrc)
	if err != nil {
		err = fmt.Errorf("Error searching data from Redis Set: %s\n", err)
		if err := sc.OutFlowUnLock(lock); err != nil {
			return 0, err
		}
		return 0, err
	}

	if err := sc.OutFlowUnLock(lock); err != nil {
		return 0, err
	}

	if ip == nil {
		outPort, err := sc.GetFirstAvailablePort(it.port)
		if err != nil {
			return 0, err
		}

		lock, err := sc.OutFlowLock(it.port)
		if err != nil {
			return 0, fmt.Errorf("Error obtaining OutFlowLock: %s\n", err, it.addr, it.port)
		}

		if err := sc.SetOutAddrPortCouple(inCrc, lIp, outPort); err != nil {
			return 0, err
		}

		if err := sc.OutFlowUnLock(lock); err != nil {
			return 0, err
		}

		go func() {
			addrSlice := make([]byte, len([]byte(*lIp))+len([]byte(ot.addr)))
			addrSlice = append(addrSlice, []byte(*lIp)...)
			addrSlice = append(addrSlice, []byte(ot.addr)...)
			outCrc := crc16.ChecksumIBM(addrSlice)

			lock, err := sc.InFlowLock(ot.port)
			if err != nil {
				utils.Log.Println("Error acquiring InFlowLock: %s\n", err)
				return
			}
			sc.SetInAddrPortCouple(outCrc, &it.addr, it.port)
			if err := sc.InFlowUnLock(lock); err != nil {
				utils.Log.Println("Error releasing InFlowLock: %s\n", err)
			}
		}()

		return outPort, nil
	} else {
		return port, nil
	}
}

func getInnerFlowChan(ot, it tuple) (*net.IP, uint16, error) {
	addrSlice := make([]byte, len([]byte(*lIp))+len([]byte(it.addr)))
	addrSlice = append(addrSlice, []byte(*lIp)...)
	addrSlice = append(addrSlice, []byte(it.addr)...)
	cks := crc16.ChecksumIBM(addrSlice)

	lock, err := sc.InFlowLock(ot.port)
	if err != nil {
		return nil, 0, fmt.Errorf("Error obtaining InFlowLock: %s\n", err)
	}

	ip, port, err := sc.GetInAddrPortCouple(cks)
	if err != nil {
		err = sc.InFlowUnLock(lock)
		if err != nil {
			utils.Log.Println("Error releasing InFlow lock: %s\n", err)
		}
		return nil, 0, fmt.Errorf("Error searching data from Redis Set: %s\n", err)
	}

	if err = sc.InFlowUnLock(lock); err != nil {
		utils.Log.Println("Error releasing InFlow lock: %s\n", err)
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
		utils.Log.Printf("Sender %d instantiated\n", i)
	}
}

func handlePacket(ipPkt []byte, pktChan chan packet) error {
	pkt := header.IPv4(ipPkt)
	if pkt == nil || len(pkt) == 0 || pkt.Payload() == nil || len(pkt.Payload()) == 0 {
		return fmt.Errorf("Error packet provided in input is null or empty!")
	}

	tc, err := getTupleFromUDPPacket(pkt, header.UDP(pkt.Payload()))
	if err != nil {
		return err
	}

	ipAddr := net.IPv4(ipPkt[12], ipPkt[13], ipPkt[14], ipPkt[15])

	utils.Log.Printf("Packet received from %s:%d headed to %s:%d\n", tc.in.addr, tc.in.port, tc.out.addr, tc.out.port)

	// TO BE REMOVED - START
	ip := net.IPv4(192, 168, 1, 102)
	destIp := ipv4ToInt32(&ip)
	// TO BE REMOVED - END

	if isInSameSubnet(&ipAddr) && destIp != ipv4ToInt32(&ipAddr) {
		port, err := getOuterFlowChan(*tc.in, *tc.out)
		if err != nil {
			utils.Log.Println(err)
			return nil
		}

		pktChan <- packet{pktBuff: pkt, srcAddr: *lIp, srcPort: port, trgAddr: tc.out.addr, trgPort: tc.out.port}
	} else {
		ip, port, err := getInnerFlowChan(*tc.out, *tc.in)
		if err != nil {
			utils.Log.Println(err)
			return nil
		}

		if ip == nil && port == 0 {
			// filter incoming packet
			return nil
		}

		optPkt := changeFieldsInPacket(packet{pktBuff: pkt, srcAddr: tc.in.addr, srcPort: tc.in.port, trgAddr: *ip, trgPort: port})

		pktChan <- packet(optPkt)
	}

	return nil
}

func InitNat() {
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

	utils.Log.Printf("Local IP address resolved to: %s\n", lIpStr)
	utils.Log.Printf("Netmask resolved to: %s\n", netmask)

	pktChan = make(chan packet, 100)
	cMap = new(chanMap)
	cMap.im = make(map[uint16]*chanMapVal)
	cMap.InitSenders(10, pktChan)
	sc = natdb.New(getGatewayIP().String(), 6379)
}

func StartUDPNat(listenPort int) {
	InitNat()
	defer sc.Close()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{net.IPv4(0, 0, 0, 0), listenPort, ""})
	if err != nil {
		utils.Log.Printf("Error instantiating UDP NAT: %s\n", err)
		return
	}
	defer conn.Close()

	for {
		buff := make([]byte, 65535)
		size, err := conn.Read(buff)
		if err != nil {
			utils.Log.Printf("Error reading from UDP socket: %s\n", err)
			continue
		}

		go handlePacket(buff[:size], pktChan)
	}

	sc.CleanUpSets()
}

func StartIPInterface(listenPort uint16) {
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

		go handlePacket(buff[:size], pktChan)
	}

}

func addressToIPv4(addr tcpip.Address) *net.IP {
	buff := []byte(addr)
	res := net.IPv4(buff[0], buff[1], buff[2], buff[3])
	return &res
}

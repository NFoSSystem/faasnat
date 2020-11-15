package context

import (
	"fmt"
	"log"
	"net"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	"github.com/piaohao/godis"
)

const (
	LOWERPORT_SET_NAME = "lowerPorts"
	LOWERPORT_SET_LOCK = "lLock"
	UPPERPORT_SET_NAME = "upperPorts"
	UPPERPORT_SET_LOCK = "uLock"
	INFLOW_SET_NAME    = "inFlow"
	INFLOW_SET_LOCK    = "inFlowLock"
	OUTFLOW_SET_NAME   = "outFlow"
	OUTFLOW_SET_LOCK   = "outFlowLock"
)

type Context struct {
	redis    *godis.Redis
	locker   *godis.Locker
	outHLock *godis.Lock
	inHLock  *godis.Lock
}

func New(hostname string, port uint16) (*Context, error) {
	c := new(Context)
	c.redis = godis.NewRedis(&godis.Option{
		Host: hostname,
		Port: int(port),
		Db:   0,
	})
	if c.redis == nil {
		return nil, fmt.Errorf("Error opening connection to Redis headed to localhost:6379")
	}
	return c, nil
}

func (c *Context) Close() error {
	return c.redis.Close()
}

func (c *Context) OutFlowLock() error {
	lock, err := c.locker.TryLock(OUTFLOW_SET_LOCK)
	if err != nil {
		return fmt.Errorf("Error acquiring %s lock: %s\n", INFLOW_SET_LOCK, err)
	}
	c.outHLock = lock
	return nil
}

func (c *Context) OutFlowUnLock() error {
	return c.locker.UnLock(c.outHLock)
}

func (c *Context) InFlowLock() error {
	lock, err := c.locker.TryLock(INFLOW_SET_LOCK)
	if err != nil {
		return fmt.Errorf("Error acquiring %s lock: %s\n", INFLOW_SET_LOCK, err)
	}
	c.outHLock = lock
	return nil
}

func (c *Context) InFlowUnLock() error {
	return c.locker.UnLock(c.inHLock)
}

func (c *Context) SetOutAddrPortCouple(crc16 uint16, addr *net.IP, port uint16) error {
	_, err := c.redis.Set("out:"+strconv.Itoa(int(crc16)), fmt.Sprintf("%s:%d", addr, port))
	return err
}

func (c *Context) GetOutAddrPortCouple(crc16 uint16) (*net.IP, uint16, error) {
	res, err := c.redis.Get("out:" + strconv.Itoa(int(crc16)))
	if err != nil {
		return nil, 0, err
	}

	if res == "" {
		return nil, 0, nil
	}

	pos := strings.Index(res, ":")
	if pos == -1 {
		return nil, 0, fmt.Errorf("Error in string format obtained from %s", OUTFLOW_SET_NAME)
	}

	ip := net.ParseIP(res[:pos])
	port, err := strconv.Atoi(res[pos+1:])
	if err != nil {
		return nil, 0, fmt.Errorf("Error in string format obtained from %s", OUTFLOW_SET_NAME)
	}

	return &ip, uint16(port), nil
}

func (c *Context) SetInAddrPortCouple(crc16 uint16, addr *net.IP, port uint16) error {
	_, err := c.redis.Set("in:"+strconv.Itoa(int(crc16)), fmt.Sprintf("%s:%d", addr, port))
	return err
}

func (c *Context) GetInAddrPortCouple(crc16 uint16) (*net.IP, uint16, error) {
	res, err := c.redis.Get("in:" + strconv.Itoa(int(crc16)))
	if err != nil {
		return nil, 0, err
	}

	if res == "" {
		return nil, 0, nil
	}

	pos := strings.Index(res, ":")
	if pos == -1 {
		return nil, 0, fmt.Errorf("Error in string format obtained from %s", OUTFLOW_SET_NAME)
	}

	ip := net.ParseIP(res[:pos])
	port, err := strconv.Atoi(res[pos+1:])
	if err != nil {
		return nil, 0, fmt.Errorf("Error in string format obtained from %s", OUTFLOW_SET_NAME)
	}

	return &ip, uint16(port), nil
}

func (c *Context) GetFirstAvailablePort(srcPort uint16) uint16 {
	var bPP *godis.BitPosParams
	var bitSetKey string
	var lock string

	if srcPort < 1024 {
		bPP := getBitPosParams(0, 1023)
		bitSetKey = LOWERPORT_SET_NAME
		lock = LOWERPORT_SET_LOCK
	} else {
		bPP := getBitPosParams(0, 64510)
		bitSetKey = LOWERPORT_SET_NAME
		lock = LOWERPORT_SET_LOCK
	}

	rLock, err := c.locker.TryLock(lock)
	pos, err := c.redis.BitPos(bitSetKey, false, bPP)
	if err != nil {
		log.Printf("Error reading pos from Redis Set: %s\n", err)
	}

	_, err = c.redis.SetBitWithBool("lowerPorts", pos, true)
	if err != nil {
		log.Printf("Error setting bit %s on lowerPorts: %s\n", err)
	}

	c.locker.UnLock(rLock)
}

func getBitPosParams(start, end int) *godis.BitPosParams {
	bPP := new(godis.BitPosParams)
	var paramsArray [][]byte = [][]byte{godis.IntToByteArr(start), godis.IntToByteArr(end)}

	rBPP := reflect.ValueOf(bPP).Elem()
	rBPPp := rBPP.Field(0)

	rParamsArray := reflect.ValueOf(&paramsArray).Elem()

	rBPPp = reflect.NewAt(rBPPp.Type(), unsafe.Pointer(rBPPp.UnsafeAddr())).Elem()

	rBPPp.Set(rParamsArray)

	return bPP
}

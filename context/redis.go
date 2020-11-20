package context

import (
	"fmt"
	"log"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"
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

	locker := godis.NewLocker(&godis.Option{
		Host: hostname,
		Port: int(port),
		Db:   0,
	}, &godis.LockOption{})

	c.locker = locker
	return c, nil
}

func (c *Context) Close() error {
	return c.redis.Close()
}

func (c *Context) OutFlowLock(dbgStr string) error {
	lock, err := c.locker.TryLock(OUTFLOW_SET_LOCK)
	if err != nil {
		return fmt.Errorf("Error acquiring %s lock: %s\n", OUTFLOW_SET_LOCK, err)
	}
	log.Println(fmt.Sprintf("In lock %s %d", dbgStr, time.Now().UnixNano()))
	c.outHLock = lock
	return nil
}

func (c *Context) OutFlowUnLock(dbgStr string) error {
	err := c.locker.UnLock(c.outHLock)
	log.Println(fmt.Sprintf("Out lock %s %d", dbgStr, time.Now().UnixNano()))
	return err
}

func (c *Context) InFlowLock(debugStr string) error {
	lock, err := c.locker.TryLock(INFLOW_SET_LOCK)
	if err != nil {
		return fmt.Errorf("DEBUG debugStr %s Error acquiring %s lock: %s\n", INFLOW_SET_LOCK, err)
	}
	c.inHLock = lock
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
	log.Printf("DEBUG key -> %s\n", "out:"+strconv.Itoa(int(crc16)))
	res, err := c.redis.Exists("out:" + strconv.Itoa(int(crc16)))
	if err != nil {
		return nil, 0, err
	}

	if res == 0 {
		return nil, 0, nil
	}

	resStr, err := c.redis.Get("out:" + strconv.Itoa(int(crc16)))
	if err != nil {
		return nil, 0, err
	}

	pos := strings.Index(resStr, ":")
	if pos == -1 {
		return nil, 0, fmt.Errorf("Error in string format obtained from %s", OUTFLOW_SET_NAME)
	}

	ip := net.ParseIP(resStr[:pos])
	port, err := strconv.Atoi(resStr[pos+1:])
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

func (c *Context) GetFirstAvailablePort(srcPort uint16) (uint16, error) {
	//var bPP *godis.BitPosParams
	var bitSetKey string
	var lock string

	if srcPort < 1024 {
		//bPP = getBitPosParams(0, 1023)
		bitSetKey = LOWERPORT_SET_NAME
		lock = LOWERPORT_SET_LOCK
	} else {
		//bPP = getBitPosParams(0, 64511)
		bitSetKey = UPPERPORT_SET_NAME
		lock = UPPERPORT_SET_LOCK
	}

	rLock, err := c.locker.TryLock(lock)
	if err != nil {
		return 0, fmt.Errorf("Error acquiring %s lock from Redis: %s")
	}
	pos, err := c.redis.BitPos(bitSetKey, false, &godis.BitPosParams{})
	if err != nil {
		log.Printf("Error reading pos from Redis Set: %s\n", err)
	}

	_, err = c.redis.SetBitWithBool(bitSetKey, pos, true)
	if err != nil {
		log.Printf("Error setting bit %d on lowerPorts: %s\n", pos, err)
	}

	if srcPort > 1023 {
		pos += 1024
	}

	c.locker.UnLock(rLock)
	log.Printf("DEBUG Available port returned: %d", pos)
	return uint16(pos), nil
}

func (ctx *Context) InitPortsBitSet() {
	if err := ctx.initPortsBitSetHelper(LOWERPORT_SET_NAME, LOWERPORT_SET_LOCK, "\x80"); err != nil {
		log.Fatalf("%s\n", err)
	}

	if err := ctx.initPortsBitSetHelper(UPPERPORT_SET_NAME, UPPERPORT_SET_LOCK, "\x00"); err != nil {
		log.Fatalf("%s\n", err)
	}
}

func (ctx *Context) initPortsBitSetHelper(setName, lockName, val string) error {
	l, err := ctx.locker.TryLock(lockName)
	if err != nil {
		return fmt.Errorf("Error acquiring Redis lock %s: %s", lockName, err)
	}

	res, err := ctx.redis.Exists(setName)
	if err != nil {
		return fmt.Errorf("Error reading value from Redis: %s", err)
	}

	if res == 0 {
		_, err = ctx.redis.Set(setName, val)
		if err != nil {
			return fmt.Errorf("Error setting key %s on Redis: %s", setName, err)
		}
		log.Printf("Set %s initialized to %s\n", setName, val)
	} else {
		log.Printf("Value of set %s equal to %s\n", setName, val)
	}

	ctx.locker.UnLock(l)
	return nil
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

func (ctx *Context) CleanUpSets() {
	if err := ctx.cleanUpSetHelper(LOWERPORT_SET_NAME, LOWERPORT_SET_LOCK); err != nil {
		log.Println(err)
	}

	if err := ctx.cleanUpSetHelper(UPPERPORT_SET_NAME, UPPERPORT_SET_LOCK); err != nil {
		log.Println(err)
	}

	if err := ctx.cleanUpSetHelper(INFLOW_SET_NAME, INFLOW_SET_LOCK); err != nil {
		log.Println(err)
	}

	if err := ctx.cleanUpSetHelper(OUTFLOW_SET_NAME, OUTFLOW_SET_LOCK); err != nil {
		log.Println(err)
	}
}

func (ctx *Context) cleanUpSetHelper(setName, lockName string) error {
	l, err := ctx.locker.TryLock(lockName)
	if err != nil {
		return fmt.Errorf("Error acquiring Redis lock %s: %s", lockName, err)
	}

	_, err = ctx.redis.Del(setName)
	if err != nil {
		return fmt.Errorf("Error deleating Redis key %s: %s", setName, err)
	}

	ctx.locker.UnLock(l)
	return nil
}

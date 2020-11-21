package natdb

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/bsm/redislock"
	"github.com/go-redis/redis/v8"
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

type SharedContext struct {
	redis  *redis.Client
	locker *redislock.Client
	ctx    context.Context
	lCtx   context.Context
	uCtx   context.Context
}

func New(hostname string, port uint16) *SharedContext {
	sc := new(SharedContext)
	sc.ctx = context.Background()
	sc.lCtx = context.Background()
	sc.uCtx = context.Background()

	sc.redis = redis.NewClient(&redis.Options{
		Network: "tcp",
		Addr:    hostname + ":" + strconv.Itoa(int(port)),
	})

	sc.locker = redislock.New(sc.redis)

	return sc
}

func (sc *SharedContext) Close() error {
	return sc.redis.Close()
}

func (sc *SharedContext) OutFlowLock(port uint16) (*redislock.Lock, error) {
	bucket := GetBitSetBucket(port)
	lock, err := sc.locker.Obtain(sc.ctx, OUTFLOW_SET_LOCK+":"+strconv.Itoa(bucket), 5*time.Second, nil)
	if err != nil {
		return nil, err
	}

	return lock, nil
}

func (sc *SharedContext) OutFlowUnLock(lock *redislock.Lock) error {
	return lock.Release(sc.ctx)
}

func (sc *SharedContext) InFlowLock(port uint16) (*redislock.Lock, error) {
	lock, err := sc.locker.Obtain(sc.ctx, INFLOW_SET_LOCK+":"+strconv.Itoa(int(port)), 5*time.Second, nil)
	if err != nil {
		return nil, err
	}

	return lock, err
}

func (sc *SharedContext) InFlowUnLock(lock *redislock.Lock) error {
	return lock.Release(sc.ctx)
}

func (sc *SharedContext) SetOutAddrPortCouple(crc16 uint16, addr *net.IP, port uint16) error {
	return sc.redis.Set(sc.ctx, "out:"+strconv.Itoa(int(crc16)), fmt.Sprintf("%s:%d", addr, port), 0).Err()
}

func (sc *SharedContext) GetOutAddrPortCouple(crc16 uint16) (*net.IP, uint16, error) {
	res, err := sc.redis.Get(sc.ctx, "out:"+strconv.Itoa(int(crc16))).Result()
	if err != nil && err != redis.Nil {
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

func (sc *SharedContext) SetInAddrPortCouple(crc16 uint16, addr *net.IP, port uint16) error {
	return sc.redis.Set(sc.ctx, "in:"+strconv.Itoa(int(crc16)), fmt.Sprintf("%s:%d", addr, port), 0).Err()
}

func (sc *SharedContext) GetInAddrPortCouple(crc16 uint16) (*net.IP, uint16, error) {
	res, err := sc.redis.Get(sc.ctx, "in:"+strconv.Itoa(int(crc16))).Result()
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

func (sc *SharedContext) GetFirstAvailablePort(srcPort uint16) (uint16, error) {
	bucket := GetBitSetBucket(srcPort)
	lockStr := "portsLock:" + strconv.Itoa(bucket)
	bitSet := "ports:" + strconv.Itoa(bucket)

	lock, err := sc.locker.Obtain(sc.ctx, lockStr, 5*time.Second, nil)
	if err != nil {
		return 0, fmt.Errorf("Error acquiring %s lock from Redis: %s", lock, err)
	}

	pos, err := sc.redis.BitPos(sc.ctx, bitSet, 0, 0).Result()
	if err != nil {
		if err = lock.Release(sc.ctx); err != nil {
			return 0, fmt.Errorf("Error releasing %s lock from Redis: %s", lock, err)
		}
		return 0, fmt.Errorf("Error reading pos from Redis Set: %s", err)
	}

	err = sc.redis.SetBit(sc.ctx, bitSet, pos, 1).Err()
	if err != nil {
		return 0, fmt.Errorf("Error setting bit %d on lowerPorts: %s", pos, err)
	}

	if srcPort > 1023 {
		pos += int64(bucket)
	}

	if err = lock.Release(sc.ctx); err != nil {
		return 0, fmt.Errorf("Error releasing %s lock from Redis: %s", lockStr, err)
	}
	return uint16(pos), nil
}

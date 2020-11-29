package natdb

import (
	"fmt"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func checkParity(val int, even bool) bool {
	return (even && val%2 == 0) ||
		(!even && val%2 != 0)
}

func GetBitSetBucket(port uint16) int {
	var limit int
	if port < 1024 {
		limit = 1024
	} else {
		limit = 65536
	}

	return rand.Intn(limit) << 6
}

func (sc *SharedContext) InitBitSets() error {
	_, err := sc.redis.Set(sc.ctx, fmt.Sprintf("ports:%d", 0), "\x80", 0).Result()
	if err != nil {
		return fmt.Errorf("Error setting BitSet %d: %s\n", 0, err)
	}
	for i := 1; i < 1024; i++ {
		_, err = sc.redis.Set(sc.ctx, fmt.Sprintf("ports:%d", i), "\x00", 0).Result()
		if err != nil {
			return fmt.Errorf("Error setting BitSet %d: %s\n", i, err)
		}
	}

	return nil
}

func (sc *SharedContext) CleanUpSets() error {
	for i := 0; i < 1024; i++ {
		_, err := sc.redis.Del(sc.ctx, fmt.Sprintf("ports:%d", i)).Uint64()
		if err != nil {
			return fmt.Errorf("Error deleating Redis key %s: %s\n", fmt.Sprintf("ports:%d", i), err)
		}
	}

	return nil
}

func (sc *SharedContext) CleanUpFlowSets() error {
	sSlice, err := sc.redis.Keys(sc.ctx, "in:*").Result()
	if err != nil {
		return fmt.Errorf("Error cleaning up in flow sets from Redis: %s\n", err)
	}

	for _, keyStr := range sSlice {
		_, err = sc.redis.Del(sc.ctx, keyStr).Result()
		if err != nil {
			return fmt.Errorf("Error cleaning up in flow sets from Redis: %s\n", err)
		}
	}

	sSlice, err = sc.redis.Keys(sc.ctx, "out:*").Result()
	if err != nil {
		return fmt.Errorf("Error cleaning up out flow sets from Redis: %s\n", err)
	}

	for _, keyStr := range sSlice {
		_, err = sc.redis.Del(sc.ctx, keyStr).Result()
		if err != nil {
			return fmt.Errorf("Error cleaning up out flow sets from Redis: %s\n", err)
		}
	}

	return nil
}

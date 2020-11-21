package natdb

import (
	"fmt"
	"log"
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

func (sc *SharedContext) InitBitSets() {
	_, err := sc.redis.Set(sc.ctx, fmt.Sprintf("ports:%d", 0), "\x80", 0).Result()
	if err != nil {
		log.Printf("Error setting BitSet %d\n", 0)
	}
	for i := 1; i < 1024; i++ {
		_, err = sc.redis.Set(sc.ctx, fmt.Sprintf("ports:%d", i), "\x00", 0).Result()
		if err != nil {
			log.Printf("Error setting BitSet %d\n", i)
		}
	}
}

func (sc *SharedContext) CleanUpSets() error {
	for i := 0; i < 1024; i++ {
		_, err := sc.redis.Del(sc.ctx, fmt.Sprintf("ports:%d", i)).Uint64()
		if err != nil {
			return fmt.Errorf("Error deleating Redis key %s: %s", fmt.Sprintf("ports:%d", i), err)
		}
	}

	return nil
}

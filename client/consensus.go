package client

import (
	"bytes"
	"encoding/gob"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/PomeloCloud/BFTRaft4go/server"
	"hash/fnv"
)

func HashData(data []byte) uint64 {
	fnv_hasher := fnv.New64a()
	fnv_hasher.Write(data)
	return fnv_hasher.Sum64()
}

func GetBytes(key interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(key)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func PickMajority(res []interface{}) interface{} {
	valMap := map[uint64]interface{}{}
	countMap := map[uint64]int{}
	for _, i := range res {
		ibytes, err := GetBytes(i)
		if err != nil {
			continue
		}
		hash := HashData(ibytes)
		valMap[hash] = i
		num, found := countMap[hash]
		if found {
			countMap[hash] = num + 1
		} else {
			countMap[hash] = 1
		}
	}
	expectedConsensus := len(res) / 2
	for hash, count := range countMap {
		if count > expectedConsensus {
			return valMap[hash]
		}
	}
	return nil
}

func MajorityResponse(clients []*spb.BFTRaftClient, f func(client *spb.BFTRaftClient) interface{}) interface{} {
	serverResChan := make(chan interface{})
	for _, c := range clients {
		go func() {
			serverResChan <- f(c)
		}()
	}
	serverRes := []interface{}{}
	for i := 0; i < len(clients); i++ {
		serverRes = append(serverRes, <- serverResChan)
	}
	return PickMajority(serverRes)
}

package utils

import (
	"bytes"
	"encoding/gob"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"hash/fnv"
)

type FuncResult struct {
	result interface{}
	feature []byte
}

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

func PickMajority(hashes []uint64) uint64 {
	countMap := map[uint64]int{}
	for _, hash := range hashes {
		num, found := countMap[hash]
		if found {
			countMap[hash] = num + 1
		} else {
			countMap[hash] = 1
		}
	}
	expectedConsensus := len(hashes) / 2
	for hash, count := range countMap {
		if count > expectedConsensus {
			return hash
		}
	}
	return 0
}

func MajorityResponse(clients []spb.BFTRaftClient, f func(client spb.BFTRaftClient) (interface{}, []byte)) interface{} {
	serverResChan := make(chan FuncResult)
	for _, c := range clients {
		go func() {
			res, fea  := f(c)
			serverResChan <- FuncResult{
				result: res,
				feature: fea,
			}
		}()
	}
	hashes := []uint64{}
	vals := map[uint64]interface{}{}
	for i := 0; i < len(clients); i++ {
		fr := <- serverResChan
		hash := HashData(fr.feature)
		hashes = append(hashes, hash)
		vals[hash] = fr.result
	}
	majorityHash := PickMajority(hashes)
	return vals[majorityHash]
}

package utils

import (
	"bytes"
	"encoding/gob"
	spb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"hash/fnv"
	"time"
)

type FuncResult struct {
	result  interface{}
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

func MajorityResponse(clients []*spb.BFTRaftClient, f func(client spb.BFTRaftClient) (interface{}, []byte)) interface{} {
	serverResChan := make(chan FuncResult, len(clients))
	for _, c := range clients {
		if c != nil {
			dataReceived := make(chan FuncResult)
			go func() {
				res, fea := f(*c)
				dataReceived <- FuncResult{
					result:  res,
					feature: fea,
				}
			}()
			go func() {
				select {
				case res := <-dataReceived:
					serverResChan <- res
				case <-time.After(10 * time.Second):
					serverResChan <- FuncResult{
						result:  nil,
						feature: []byte{},
					}
				}
			}()
		}
	}
	hashes := []uint64{}
	vals := map[uint64]interface{}{}
	for i := 0; i < len(clients); i++ {
		fr := <-serverResChan
		if fr.result == nil {
			continue
		}
		hash := HashData(fr.feature)
		hashes = append(hashes, hash)
		vals[hash] = fr.result
	}
	majorityHash := PickMajority(hashes)
	if val, found := vals[majorityHash]; found {
		return val
	} else {
		for _, v := range vals {
			return v
		}
	}
	return nil
}

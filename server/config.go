package server

import (
	"github.com/dgraph-io/badger"
	pb "github.com/PomeloCloud/BFTRaft4go/proto"
	"github.com/golang/protobuf/proto"
)


func GetConfig(kv *badger.KV) (*pb.ServerConfig, error) {
	item := badger.KVItem{}
	if err := kv.Get(ComposeKeyPrefix(CONFIG_GROUP, SERVER_CONF), &item); err != nil {
		return nil, err
	}
	data := ItemValue(&item)
	conf := pb.ServerConfig{}
	if err := proto.Unmarshal(data, &conf); err != nil {
		return nil, err
	}
	return &conf, nil
}
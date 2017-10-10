package server

import (
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"errors"
)

func GetConfig(kv *badger.DB) (*pb.ServerConfig, error) {
	var res *pb.ServerConfig = nil
	err := kv.View(func(txn *badger.Txn) error {
		if item, err := txn.Get(ComposeKeyPrefix(CONFIG_GROUP, SERVER_CONF)); err != nil {
			return err
		} else {
			data := ItemValue(item)
			if data == nil {
				return errors.New("no data")
			} else {
				conf := pb.ServerConfig{}
				if err := proto.Unmarshal(*data, &conf); err != nil {
					return err
				}
				res = &conf
				return nil
			}
		}
	})
	return res, err
}

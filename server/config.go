package server

import (
	"errors"
	pb "github.com/PomeloCloud/BFTRaft4go/proto/server"
	"github.com/dgraph-io/badger"
	"github.com/golang/protobuf/proto"
	"io/ioutil"
	"log"
	"encoding/json"
)

func GetConfig(kv *badger.DB) (*pb.ServerConfig, error) {
	var res *pb.ServerConfig = nil
	err := kv.View(func(txn *badger.Txn) error {
		if item, err := txn.Get(ComposeKeyPrefix(CONFIG_GROUP, SERVER_CONF)); err != nil {
			log.Panic(err)
			return err
		} else {
			data := ItemValue(item)
			if data == nil {
				log.Panic(err)
				return errors.New("no data")
			} else {
				conf := pb.ServerConfig{}
				if err := proto.Unmarshal(*data, &conf); err != nil {
					log.Panic(err)
					return err
				}
				res = &conf
				return nil
			}
		}
	})
	return res, err
}

type FileConfig struct {
	Db         string
	Address    string
	Bootstraps []string
}

func ReadConfigFile(path string) FileConfig {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	log.Println(string(data))
	fc := FileConfig{}
	if err := json.Unmarshal(data, &fc); err != nil {
		panic(err)
	}
	return fc
}

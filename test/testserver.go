package main

import (
	"github.com/PomeloCloud/BFTRaft4go/server"
	"github.com/PomeloCloud/BFTRaft4go/utils"
	"os"
	"time"
	"log"
)

func initDB(dbPath string) {
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		panic(err)
	}
	server.InitDatabase(dbPath)
}

func getServer(dbPath string, addr string, bootstraps []string) *server.BFTRaftServer {
	initDB(dbPath)
	s, err := server.GetServer(server.Options{
		DBPath:           dbPath,
		Address:          addr,
		Bootstrap:        bootstraps,
		ConsensusTimeout: 5 * time.Second,
	})

	if err != nil {
		panic(err)
	}
	return s
}

func main() {
	print("testing")
	args := os.Args
	cfgPath := args[1]
	log.Println(cfgPath)
	cfg := server.ReadConfigFile(cfgPath)
	os.RemoveAll(cfg.Db)
	defer os.RemoveAll(cfg.Db)
	println("start server", cfg.Address)
	s := getServer(cfg.Db, cfg.Address, cfg.Bootstraps)
	time.Sleep(1 * time.Second)
	s.StartServer()
	if len(cfg.Bootstraps) > 0 {
		time.Sleep(1 * time.Second)
		s.NodeJoin(utils.ALPHA_GROUP)
	}
	<-time.After(1 * time.Hour)
}

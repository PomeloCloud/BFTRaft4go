package server

import (
	"testing"
	"time"
	"os"
	"github.com/PomeloCloud/BFTRaft4go/utils"
)

func initDB(dbPath string, t *testing.T) {
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	InitDatabase(dbPath)
}

func getServer(dbPath string, addr string, bootstraps []string, t *testing.T) *BFTRaftServer {
	initDB(dbPath, t)
	s, err := GetServer(Options{
		DBPath: dbPath,
		Address: addr,
		Bootstrap: bootstraps,
		ConsensusTimeout: 1 * time.Minute,
	})

	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServerStartup(t *testing.T) {
	dbPath := "test_data/ServerStartup"
	s := getServer(dbPath, "localhost:4560", []string{}, t)
	defer os.RemoveAll(dbPath)
	go func() {
		if err := s.StartServer(); err != nil {
			t.Fatal(err)
		}
	}()
}

func TestColdStart(t *testing.T) {
	// test for creating a cold started node and add a member to join it
	dbPath1 := "test_data/TestColdStart1"
	dbPath2 := "test_data/TestColdStart2"
	dbPath3 := "test_data/TestColdStart3"
	addr1 := "localhost:4561"
	addr2 := "localhost:4562"
	addr3 := "localhost:4563"
	os.RemoveAll(dbPath1)
	os.RemoveAll(dbPath2)
	os.RemoveAll(dbPath3)
	defer os.RemoveAll(dbPath1)
	defer os.RemoveAll(dbPath2)
	defer os.RemoveAll(dbPath3)

	println("start server 1")
	s1 := getServer(dbPath1, addr1, []string{}, t)
	time.Sleep(1 * time.Second)
	s1.StartServer()
	time.Sleep(1 * time.Second)

	println("start server 2")
	s2 := getServer(dbPath2, addr2, []string{addr1}, t)
	time.Sleep(1 * time.Second)
	s2.StartServer()
	time.Sleep(1 * time.Second)
	s2.NodeJoin(utils.ALPHA_GROUP)
	time.Sleep(5 * time.Second)

	println("start server 3")
	s3 := getServer(dbPath3, addr3, []string{addr1, addr2}, t)
	time.Sleep(1 * time.Second)
	s3.StartServer()
	time.Sleep(1 * time.Second)
	s3.NodeJoin(utils.ALPHA_GROUP)
	time.Sleep(10 * time.Second)
}
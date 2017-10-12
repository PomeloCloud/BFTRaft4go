package server

import (
	"testing"
	"time"
	"os"
)

func initDB(dbPath string, t *testing.T) {
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	InitDatabase(dbPath)
}

func getServer(dbPath string, addr string, t *testing.T) *BFTRaftServer {
	initDB(dbPath, t)
	s, err := GetServer(Options{
		DBPath: dbPath,
		Address: addr,
		Bootstrap: []string{},
		ConsensusTimeout: 1 * time.Minute,
	})

	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServerStartup(t *testing.T) {
	dbPath := "test_data/ServerStartup"
	s := getServer(dbPath, "localhost:4560", t)
	go func() {
		if err := s.StartServer(); err != nil {
			t.Fatal(err)
		}
	}()
	os.RemoveAll(dbPath)
}

func TestColdStart(t *testing.T) {
	// test for creating a cold started node and add a member to join it
	dbPath1 := "test_data/ServerStartup"
	addr1 := "localhost:4561"
	getServer(dbPath1, addr1, t)
	os.RemoveAll(dbPath1)
}
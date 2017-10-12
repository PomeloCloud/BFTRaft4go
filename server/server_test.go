package server

import (
	"testing"
	"time"
	"os"
)

func TestServerStartup(t *testing.T) {
	dbPath := "test_data/ServerStartup"
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		t.Fatal(err)
	}
	InitDatabase(dbPath)
	_, err := GetServer(Options{
		DBPath: "test_data/ServerStartup",
		Address: "localhost:4560",
		Bootstrap: []string{},
		ConsensusTimeout: 1 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dbPath)
}
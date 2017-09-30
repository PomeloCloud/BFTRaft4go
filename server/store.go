package server

import "encoding/binary"

const (
	RAFT_LOGS = 0
	SERVER_MEMBERS = 1
)

func ComposeKeyPrefix (group int64, t uint32) []byte {
	groupBytes := make([]byte, 8)
	typeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint64(groupBytes, uint64(group))
	binary.LittleEndian.PutUint32(typeBytes, t)
	return append(groupBytes, typeBytes...)
}


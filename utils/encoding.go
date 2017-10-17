package utils

import "encoding/binary"

const (
	UINT32_LEN = 4
	UINT64_LEN = 8
)

func U32Bytes(t uint32) []byte {
	bs := make([]byte, UINT32_LEN)
	binary.BigEndian.PutUint32(bs, t)
	return bs
}

func U64Bytes(n uint64) []byte {
	bs := make([]byte, UINT64_LEN)
	binary.BigEndian.PutUint64(bs, n)
	return bs
}

func BytesU64(bs []byte, offset int) uint64 {
	return binary.BigEndian.Uint64(bs[offset : offset+UINT64_LEN])
}

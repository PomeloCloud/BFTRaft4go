package main

import (
	"encoding/binary"
	"bytes"
	"fmt"
)

type CustomMsg interface {
	Call(req []byte) []byte
}

type T struct {
	S string
}

func (t T) Call(req []byte) []byte {
	println("It's 42 and", t.S)
	return []byte{0}
}

func main() {
	t := T{"a mouse"}
	t.Call([]byte{})

	var bin_buf bytes.Buffer
	binary.Write(&bin_buf, binary.BigEndian, t)

	bs := bin_buf.Bytes()

	var resI CustomMsg
	rbuf := bytes.NewReader(bs)
	err := binary.Read(rbuf, binary.BigEndian, &resI)
	if err != nil {
		fmt.Println("binary.Read failed:", err)
	}
	resI.Call([]byte{})
}

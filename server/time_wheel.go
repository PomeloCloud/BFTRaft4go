package server

import (
	"math/rand"
)

func RandomTimeout(mult float32) int {
	lowRange := 500 * mult
	highRange := 2000 * mult
	return int(lowRange + highRange*rand.Float32())
}

package server

import "time"

func TimeoutChan(t time.Duration) chan bool {
	c := make(chan bool, 1)
	go func() {
		time.Sleep(t)
		c <- true
	}()
	return c
}
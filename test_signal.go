package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var sysSignal = make(chan os.Signal)

func close() {
	defer fmt.Printf("closed!\n")
	signal.Notify(sysSignal,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)
	for {
		select {
		case s := <-sysSignal:
			fmt.Printf("signal: %v\n", s)
			time.Sleep(time.Second * 1)
			os.Exit(0)
		}
	}
}

func main() {
	close()
}

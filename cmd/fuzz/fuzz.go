package main

import (
	"github.com/jakecoffman/rely"
	"os"
	"strconv"
	"syscall"
	"github.com/op/go-logging"
	"os/signal"
	"fmt"
	"math/rand"
)

var globalTime float64 = 100

var endpoint rely.Endpoint

func main() {
	logging.SetLevel(logging.CRITICAL, "rely")

	numIterations := -1

	if len(os.Args) > 1 {
		var err error
		numIterations, err = strconv.Atoi(os.Args[1])
		if err != nil {
			panic("argument 2 must be an integer")
		}
	}

	initialize()

	var quit bool

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT)

	go func() {
		<-signals
		quit = true
		close(signals)
	}()

	deltaTime := .1

	if numIterations > 0 {
		for i := 0; i < numIterations; i++ {
			if quit {
				break
			}

			iteration(globalTime)
			globalTime += deltaTime
		}
	} else {
		for i := 0; !quit; i++ {
			iteration(globalTime)
			globalTime += deltaTime
		}
	}
}

func initialize() {
	config := rely.NewDefaultConfig()

	config.Index = 0
	config.TransmitPacketFunction = testTransmitPacketFunction
	config.ProcessPacketFunction = testProcessPacketFunction

	endpoint = *rely.NewEndpoint(config, globalTime)
}

func iteration(time float64) {
	fmt.Print(".")

	packetData := make([]byte, testMaxPacketBytes)
	packetBytes := rand.Intn(testMaxPacketBytes-1)+1
	for i := 0; i < packetBytes; i++ {
		packetData[i] = byte(rand.Int() % 256)
	}

	endpoint.ReceivePacket(packetData[:packetBytes])
	endpoint.Update(time)
	endpoint.ClearAcks()
}

func testTransmitPacketFunction(_ interface{}, _ int, _ uint16, _ []byte) {}

const testMaxPacketBytes = 16 * 1024

func testProcessPacketFunction(_ interface{}, _ int, _ uint16, _ []byte) bool {
	return true
}

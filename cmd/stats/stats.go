//+build test

package main

import (
	"github.com/jakecoffman/rely"
	"log"
	"os"
	"strconv"
	"syscall"
	"github.com/op/go-logging"
	"os/signal"
	"fmt"
	"math"
)

const testMaxPacketBytes = 290
var globalTime = 100.

type testContext struct {
	client *rely.Endpoint
	server *rely.Endpoint
}

var globalContext = &testContext{}

func main() {
	logging.SetLevel(logging.ERROR, "rely")

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

	deltaTime := .01

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
	clientConfig := rely.NewDefaultConfig()
	serverConfig := rely.NewDefaultConfig()

	clientConfig.FragmentAbove = testMaxPacketBytes
	serverConfig.FragmentAbove = testMaxPacketBytes

	clientConfig.Context = globalContext
	clientConfig.Name = "client"
	clientConfig.Index = 0
	clientConfig.TransmitPacketFunction = testTransmitPacketFunction
	clientConfig.ProcessPacketFunction = testProcessPacketFunction

	serverConfig.Context = globalContext
	serverConfig.Name = "server"
	serverConfig.Index = 1
	serverConfig.TransmitPacketFunction = testTransmitPacketFunction
	serverConfig.ProcessPacketFunction = testProcessPacketFunction

	globalContext.client = rely.NewEndpoint(clientConfig, globalTime)
	globalContext.server = rely.NewEndpoint(serverConfig, globalTime)
}

func generatePacketData(sequence uint16) []byte {
	packetBytes := testMaxPacketBytes
	packetData := make([]byte, packetBytes)
	packetData[0] = byte(sequence & 0xFF)
	packetData[1] = byte((sequence >> 8) & 0xFF)
	for i := 2; i < packetBytes; i++ {
		packetData[i] = byte((i + int(sequence)) % 256)
	}
	return packetData
}

func iteration(time float64) {
	{
		sequence := globalContext.client.NextPacketSequence()
		packetData := generatePacketData(sequence)
		globalContext.client.SendPacket(packetData)
	}

	{
		sequence := globalContext.server.NextPacketSequence()
		packetData := generatePacketData(sequence)
		globalContext.server.SendPacket(packetData)
	}

	globalContext.client.Update(time)
	globalContext.server.Update(time)

	globalContext.client.ClearAcks()
	globalContext.server.ClearAcks()

	sent, recved, acked := globalContext.client.Bandwidth()

	fmt.Printf("%v sent | %v received | %v acked | rtt = %vms | packet loss = %v%% | sent = %vkbps | recv = %vkbps | acked = %vkbps\n",
		globalContext.client.PacketsSent(),
		globalContext.client.PacketsReceived(),
		globalContext.client.PacketsAcked(),
		globalContext.client.Rtt(),
		int(math.Floor(globalContext.client.PacketLoss()+.5)),
		int(sent), int(recved), int(acked),
	)
}

func testTransmitPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) {
	ctx := context.(*testContext)

	if sequence%5 == 0 {
		return
	}

	if index == 0 {
		ctx.server.ReceivePacket(packetData)
	} else if index == 1 {
		ctx.client.ReceivePacket(packetData)
	}
}

func testProcessPacketFunction(_ interface{}, _ int, _ uint16, packetData []byte) bool {
	if packetData == nil || len(packetData) <= 0 || len(packetData) > testMaxPacketBytes {
		log.Fatal("invalid packet data")
	}

	if len(packetData) < 2 {
		log.Fatal("invalid packet data size")
	}

	var seq uint16
	seq |= uint16(packetData[0])
	seq |= uint16(packetData[1]) << 8
	if len(packetData) != testMaxPacketBytes {
		log.Fatal("Size not right, expected ", testMaxPacketBytes, " got ", len(packetData))
	}
	for i := 2; i < len(packetData); i++ {
		if packetData[i] != byte((i+int(seq))%256) {
			log.Fatal("Wrong packet data at index ", i, " got ", packetData[i], " expected ", (i+int(seq))%256)
		}
	}

	return true
}

//+build test

package main

import (
	"os"
	"syscall"
	"os/signal"
	"github.com/jakecoffman/rely"
	"math/rand"
	"log"
	"github.com/op/go-logging"
	"runtime/pprof"
	"flag"
)

var globalTime float64 = 100

type testContext struct {
	client *rely.Endpoint
	server *rely.Endpoint
}

var globalContext = testContext{}

// to profile, run `./soak -cpuprofile=prof -iterations=8000`, then run `go tool pprof soak profile`
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var iterations = flag.Int("iterations", -1, "number of iterations to run")
var loglevel = flag.Int("loglevel", int(logging.ERROR), "log level (5 for debug)")

func main() {
	flag.Parse()

	logging.SetLevel(logging.Level(*loglevel), "rely")

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
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

	if *iterations > 0 {
		for i := 0; i < *iterations; i++ {
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

	clientConfig.FragmentAbove = 500
	serverConfig.FragmentAbove = 500

	clientConfig.Context = &globalContext
	clientConfig.Name = "client"
	clientConfig.Index = 0
	clientConfig.TransmitPacketFunction = testTransmitPacketFunction
	clientConfig.ProcessPacketFunction = testProcessPacketFunction

	serverConfig.Context = &globalContext
	serverConfig.Name = "server"
	serverConfig.Index = 1
	serverConfig.TransmitPacketFunction = testTransmitPacketFunction
	serverConfig.ProcessPacketFunction = testProcessPacketFunction

	globalContext.client = rely.NewEndpoint(clientConfig, globalTime)
	globalContext.server = rely.NewEndpoint(serverConfig, globalTime)
}

func testTransmitPacketFunction(context interface{}, index int, _ uint16, packetData []byte) {
	ctx := context.(*testContext)

	if rand.Intn(100) < 5 {
		return
	}

	if index == 0 {
		ctx.server.ReceivePacket(packetData)
	} else if index == 1 {
		ctx.client.ReceivePacket(packetData)
	}
}

const testMaxPacketBytes = 16*1024

func testProcessPacketFunction(_ interface{}, _ int, _ uint16, packetData []byte) bool{
	if packetData == nil || len(packetData) <= 0 || len(packetData) >= testMaxPacketBytes {
		log.Fatal("invalid packet data")
	}

	if len(packetData) < 2 {
		log.Fatal("invalid packet data size")
	}

	var seq uint16
	seq |= uint16(packetData[0])
	seq |= uint16(packetData[1]) << 8
	if len(packetData) < ((int(seq)*1023)%(testMaxPacketBytes-2))+2 {
		log.Fatal("Size not right, expected ", ((int(seq)*1023)%(testMaxPacketBytes-2))+2, " got ", len(packetData))
	}
	for i := 2; i < len(packetData); i++ {
		if packetData[i] != byte((i+int(seq))%256) {
			log.Fatal("Wrong packet data at index ", i, " got ", packetData[i], " expected ", (i+int(seq))%256)
		}
	}

	return true
}

func generatePacketData(sequence uint16) []byte {
	packetBytes := ((int(sequence)*1023) % (testMaxPacketBytes - 2)) + 2
	if packetBytes < 2 || packetBytes > testMaxPacketBytes {
		log.Fatal("failed to gen packetBytes", packetBytes)
	}
	packetData := make([]byte, packetBytes)
	packetData[0] = byte(sequence & 0xFF)
	packetData[1] = byte((sequence >> 8) & 0xFF)
	for i := 2; i < packetBytes; i++ {
		packetData[i] = byte((i+int(sequence))%256)
	}
	return packetData[:packetBytes]
}

func iteration(time float64) {
	sequence := globalContext.client.NextPacketSequence()
	packetData := generatePacketData(sequence)
	globalContext.client.SendPacket(packetData)

	sequence = globalContext.server.NextPacketSequence()
	packetData = generatePacketData(sequence)
	globalContext.server.SendPacket(packetData)

	globalContext.client.Update(time)
	globalContext.server.Update(time)

	globalContext.client.ClearAcks()
	globalContext.server.ClearAcks()
}

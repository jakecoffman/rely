package main

import (
	"github.com/jakecoffman/rely"
	"time"
	"log"
	"math/rand"
	"flag"
	"net"
	"fmt"
	"math"
	"bytes"
)

var endpoint *rely.Endpoint
var globalTime = float64(time.Now().UnixNano()) / (1000 * 1000 * 1000)

var name = flag.String("name", "server", "name of connection")
var addr = flag.String("addr", "0.0.0.0:8987", "host and port of connection")

// used by server
var packetConn net.PacketConn
var clients = map[string]net.Addr{}

// used by clients
var conn net.Conn

var incoming = make(chan []byte, 1000)

func main() {
	// TODO: must be a bug somewhere... this should be 1024
	const bufferSize = 1038

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	config := rely.NewDefaultConfig()
	config.Name = *name
	config.TransmitPacketFunction = transmitPacket
	config.ProcessPacketFunction = processPacket

	var err error
	if config.Name == "server" {
		config.Index = 1
		packetConn, err = net.ListenPacket("udp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		defer packetConn.Close()

		go func() {
			for {
				buffer := make([]byte, bufferSize)
				n, addr, err := packetConn.ReadFrom(buffer)
				if err != nil {
					log.Fatal(err)
				}
				clients[addr.String()] = addr
				incoming <- buffer[:n]
			}
		} ()

		log.Println("Server ready")
	} else {
		config.Index = 2
		conn, err = net.Dial("udp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		defer conn.Close()

		go func() {
			for {
				buffer := make([]byte, bufferSize)
				n, err := conn.Read(buffer)
				if err != nil {
					log.Fatal(err)
				}
				incoming <- buffer[:n]
			}
		} ()

		log.Println("Client ready")
	}

	endpoint = rely.NewEndpoint(config, globalTime)

	for {
		globalTime = float64(time.Now().UnixNano()) / (1000 * 1000 * 1000)

		processIncoming:
		for {
			select {
			case d := <-incoming:
				endpoint.ReceivePacket(d)
			default:
				break processIncoming
			}
		}

		sequence := endpoint.NextPacketSequence()
		data := generatePacketData(sequence, make([]byte, testMaxPacketBytes))
		endpoint.SendPacket(data)

		endpoint.Update(globalTime)

		endpoint.ClearAcks()

		sent, recved, acked := endpoint.Bandwidth()
		fmt.Printf("%v sent | %v received | %v acked | rtt = %vms | packet loss = %v%% | sent = %vkbps | recv = %vkbps | acked = %vkbps\n",
			endpoint.PacketsSent(),
			endpoint.PacketsReceived(),
			endpoint.PacketsAcked(),
			endpoint.Rtt(),
			int(math.Floor(endpoint.PacketLoss()+.5)),
			int(sent), int(recved), int(acked),
		)
		time.Sleep(16 * time.Millisecond)
	}
}

func transmitPacket(_ interface{}, index int, sequence uint16, packetData []byte) {
	if sequence%5 == 0 {
		return
	}

	var n int
	var err error

	if index == 1 {
		for _, addr := range clients {
			_, err = packetConn.WriteTo(packetData, addr)
			if err != nil {
				log.Fatal(err)
			}
		}
		return
	}
	n, err = conn.Write(packetData)
	if err != nil {
		log.Fatal(err)
	}
	if n < len(packetData) {
		log.Fatal("OOPS")
	}
}

const testMaxPacketBytes = 16*1024

func processPacket(_ interface{}, _ int, _ uint16, packetData []byte) bool {
	if packetData == nil || len(packetData) <= 0 || len(packetData) >= testMaxPacketBytes {
		log.Fatal("invalid packet data")
	}

	if len(packetData) < 2 {
		log.Fatal("invalid packet data size")
	}

	var seq uint16
	seq |= uint16(packetData[0])
	seq |= uint16(packetData[1]) << 8
	expectedBytes := ((int(seq)*1023)%(testMaxPacketBytes-2))+2
	if len(packetData) != expectedBytes {
		log.Fatal("Size not right, expected ", expectedBytes, " got ", len(packetData))
	}
	expectedBuffer := make([]byte, expectedBytes)
	expectedBuffer = generatePacketData(seq, expectedBuffer)
	if bytes.Compare(packetData[2:], expectedBuffer[2:expectedBytes]) != 0 {
		log.Fatal("Wrong packet data", packetData[2:])
	}

	return true
}

func generatePacketData(sequence uint16, packetData []byte) []byte {
	packetBytes := ((int(sequence)*1023) % (testMaxPacketBytes - 2)) + 2
	if packetBytes < 2 || packetBytes > testMaxPacketBytes {
		log.Fatal("failed to gen packetBytes", packetBytes)
	}
	packetData[0] = byte(sequence & 0xFF)
	packetData[1] = byte((sequence >> 8) & 0xFF)
	for i := 2; i < packetBytes; i++ {
		packetData[i] = byte((i+int(sequence))%256)
	}
	return packetData[:packetBytes]
}

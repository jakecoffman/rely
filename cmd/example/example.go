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

var name = flag.String("name", "server", "name of connection")
var addr = flag.String("addr", "0.0.0.0:8987", "host and port of connection")

// used by server
var packetConn net.PacketConn
var clients = map[string]net.Addr{}

// used by clients
var conn net.Conn

const tickrate = 20
const packetByteSize = 1024/tickrate

var incoming = make(chan []byte, 1000)
var packetData = map[uint16][]byte{}

func main() {
	const bufferSize = packetByteSize + rely.MaxPacketHeaderBytes

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

		// wait for first connection
		incoming<- <-incoming
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

	endpoint = rely.NewEndpoint(config, now())

	networkTick := time.NewTicker(time.Second/tickrate)

	for {
		// process all incoming packets
		processIncoming:
		for {
			endpoint.Update(now())

			select {
			case d := <-incoming:
				endpoint.ReceivePacket(d)
			case <-networkTick.C:
				break processIncoming
			}
		}
		t := now()
		endpoint.Update(t)

		// clear the stored packets that have been acked
		acks := endpoint.GetAcks()
		for _, sequence := range acks {
			delete(packetData, sequence)
		}
		endpoint.ClearAcks()

		// resend packets that haven't been acked in over 150ms
		for sequence, data := range packetData {
			packet := endpoint.SentPackets.Find(sequence)
			if packet == nil {
				// probably the packet was too old and was dropped?
				delete(packetData, sequence)
				continue
			}

			if t - packet.Time > .15 {
				fmt.Println("Resending packet", sequence)
				endpoint.SendPacket(data)
			}
		}

		// send new updates
		sequence := endpoint.NextPacketSequence()
		data := generatePacketData(sequence, make([]byte, packetByteSize))
		endpoint.SendPacket(data)
		packetData[sequence] = data

		sent, recved, acked := endpoint.Bandwidth()
		fmt.Printf("%v sent | %v received | %v acked | rtt = %vms | packet loss = %v%% | sent = %vkbps | recv = %vkbps | acked = %vkbps\n",
			endpoint.PacketsSent(),
			endpoint.PacketsReceived(),
			endpoint.PacketsAcked(),
			endpoint.Rtt(),
			int(math.Floor(endpoint.PacketLoss()+.5)),
			int(sent), int(recved), int(acked),
		)
		if int(math.Floor(endpoint.PacketLoss()+.5)) > 10 {
			return
		}
	}
}

func transmitPacket(_ interface{}, index int, _ uint16, packetData []byte) {
	var n int
	var err error

	if rand.Intn(100) == 0 {
		// 1% packet loss
		return
	}

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

func processPacket(_ interface{}, _ int, _ uint16, packetData []byte) bool {
	if packetData == nil || len(packetData) != packetByteSize {
		log.Fatal("invalid packet data")
	}

	if len(packetData) < 2 {
		log.Fatal("invalid packet data size")
	}

	var seq uint16
	seq |= uint16(packetData[0])
	seq |= uint16(packetData[1]) << 8
	expectedBytes := packetByteSize
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
	packetBytes := packetByteSize
	packetData[0] = byte(sequence & 0xFF)
	packetData[1] = byte((sequence >> 8) & 0xFF)
	for i := 2; i < packetBytes; i++ {
		packetData[i] = byte((i+int(sequence))%256)
	}
	return packetData[:packetBytes]
}

func now() float64 {
	return float64(time.Now().UnixNano()) / (1000 * 1000 * 1000)
}

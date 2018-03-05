package rely

import (
	"testing"
	l "log"
	"github.com/op/go-logging"
)

func TestPacketHeader(t *testing.T) {
	var writeSequence, writeAck, readSequence, readAck uint16
	var writeAckBits, readAckBits uint32

	packetData := make([]byte, maxPacketHeaderBytes)

	// worst case, sequence and ack are far apart, no packets acked

	writeSequence = 10000
	writeAck = 100
	writeAckBits = 0

	bytesWritten := WritePacketHeader(packetData, writeSequence, writeAck, writeAckBits)
	if bytesWritten != maxPacketHeaderBytes {
		t.Error("Should have written", maxPacketHeaderBytes, "but got", bytesWritten)
	}

	bytesRead := ReadPacketHeader("test_packet_header", packetData, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// rare case. sequence and ack are far apart, significant # of acks are missing

	writeSequence = 10000
	writeAck = 100
	writeAckBits = 0xFEFEFFFE

	bytesWritten = WritePacketHeader(packetData, writeSequence, writeAck, writeAckBits)
	if bytesWritten != 1+2+2+3 {
		t.Error(bytesWritten, "!=", 1+2+2+3)
	}

	bytesRead = ReadPacketHeader("test_packet_header", packetData, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// common case under packet loss. sequence and ack are close together, some acks are missing

	writeSequence = 200
	writeAck = 100
	writeAckBits = 0xFFFEFFFF

	bytesWritten = WritePacketHeader(packetData, writeSequence, writeAck, writeAckBits)

	if bytesWritten != 1 + 2 + 1 + 1 {
		t.Error(bytesWritten, "!=", 1+2+1+1)
	}

	bytesRead = ReadPacketHeader("test_packet_header", packetData, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// ideal case. no packet loss.

	writeSequence = 200
	writeAck = 100
	writeAckBits = 0xFFFFFFFF

	bytesWritten = WritePacketHeader(packetData, writeSequence, writeAck, writeAckBits)

	if bytesWritten != 1 + 2 + 1 {
		t.Error(bytesWritten, "!=", 1+2+1)
	}

	bytesRead = ReadPacketHeader("test_packet_header", packetData, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}
}

type testContext struct {
	drop int
	sender, receiver *Endpoint
}

func testTransmitPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) {
	ctx := context.(*testContext)

	if ctx.drop != 0 {
		l.Println("DROP")
		return
	}

	if index == 0 {
		ctx.receiver.ReceivePacket(packetData)
	} else if index == 1 {
		ctx.sender.ReceivePacket(packetData)
	}
}

func testProcessPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) bool {
	return true
}

const testAcksNumIterations = 256

func TestAcks(t *testing.T) {
	logging.SetLevel(logging.ERROR, "rely")
	time := 100.0

	var context testContext

	senderConfig := NewDefaultConfig()
	receiverConfig := NewDefaultConfig()

	senderConfig.Context = &context
	senderConfig.Index = 0
	senderConfig.TransmitPacketFunction = testTransmitPacketFunction
	senderConfig.ProcessPacketFunction = testProcessPacketFunction

	receiverConfig.Context = &context
	receiverConfig.Index = 1
	receiverConfig.TransmitPacketFunction = testTransmitPacketFunction
	receiverConfig.ProcessPacketFunction = testProcessPacketFunction

	context.sender = NewEndpoint(senderConfig, time)
	context.receiver = NewEndpoint(receiverConfig, time)

	deltaTime := 0.01

	for i := 0; i < testAcksNumIterations; i ++ {
		dummyPacket := []byte{1, 2, 3, 4, 5, 6, 7, 8,}

		context.sender.SendPacket(dummyPacket)
		context.receiver.SendPacket(dummyPacket)

		context.sender.Update(time)
		context.receiver.Update(time)

		time += deltaTime
	}

	senderAckedPacket := make([]uint8, testAcksNumIterations)
	numSenderAcks, senderAcks := context.sender.GetAcks()
	for i := 0; i < numSenderAcks; i++ {
		if senderAcks[i] < testAcksNumIterations {
			senderAckedPacket[senderAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations / 2; i++ {
		if senderAckedPacket[i] != 1 {
			t.Fatal("Packet not acked", i)
		}
	}

	receiverAckedPacket := make([]uint8, testAcksNumIterations)
	numReceiverAcks, receiverAcks := context.receiver.GetAcks()
	for i := 0; i < numReceiverAcks; i++ {
		if receiverAcks[i] < testAcksNumIterations {
			receiverAckedPacket[receiverAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations / 2; i++ {
		if receiverAckedPacket[i] != 1 {
			t.Fatal("Packet not acked", i)
		}
	}
}

func TestAcksPacketLoss(t *testing.T) {
	time := 100.0

	context := testContext{}
	senderConfig := NewDefaultConfig()
	receiverConfig := NewDefaultConfig()

	senderConfig.Context = &context
	senderConfig.Index = 0
	senderConfig.TransmitPacketFunction = testTransmitPacketFunction
	senderConfig.ProcessPacketFunction = testProcessPacketFunction

	receiverConfig.Context = &context
	receiverConfig.Index = 0
	receiverConfig.TransmitPacketFunction = testTransmitPacketFunction
	receiverConfig.ProcessPacketFunction = testProcessPacketFunction

	context.sender = NewEndpoint(senderConfig, time)
	context.receiver = NewEndpoint(receiverConfig, time)

	deltaTime := 0.1
	for i := 0; i < testAcksNumIterations; i++ {
		dummyPacket := make([]uint8, 8)

		context.drop = i % 2

		context.sender.SendPacket(dummyPacket)
		context.receiver.SendPacket(dummyPacket)

		context.sender.Update(time)
		context.receiver.Update(time)

		time += deltaTime
	}

	senderAckedPacket := make([]uint8, testAcksNumIterations)
	numSenderAcks, senderAcks := context.sender.GetAcks()
	for i := 0; i < numSenderAcks; i++ {
		if senderAcks[i] < testAcksNumIterations {
			senderAckedPacket[senderAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations/2; i++ {
		if senderAckedPacket[i] != uint8((i+1) % 2) {
			t.Error("Acked packet wrong:", i)
		}
	}
}

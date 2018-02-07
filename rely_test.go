package rely

import (
	"testing"
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
	drop bool
	sender, receiver *Endpoint
}

func testTransmitPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) {
	ctx := context.(*testContext)

	if ctx.drop {
		return
	}

	if index == 0 {
		ctx.receiver.ReceivePacket(packetData)
	} else if index == 1 {
		ctx.sender.ReceivePacket(packetData)
	}
}

func processPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) bool {
	return true
}

const testAcksNumIterations = 256

func TestAcks(t *testing.T) {
	time := 100.0

	var context testContext

	senderConfig := NewDefaultConfig()
	receiverConfig := NewDefaultConfig()

	senderConfig.Context = &context
	senderConfig.Index = 0
	senderConfig.TransmitPacketFunction = testTransmitPacketFunction
	senderConfig.ProcessPacketFunction = processPacketFunction

	receiverConfig.Context = &context
	receiverConfig.Index = 1
	receiverConfig.TransmitPacketFunction = testTransmitPacketFunction
	receiverConfig.ProcessPacketFunction = processPacketFunction

	context.sender = NewEndpoint(senderConfig, time)
	context.receiver = NewEndpoint(receiverConfig, time)

	deltaTime := 0.01

	for i := 0; i < testAcksNumIterations; i ++ {
		dummyPacket := make([]byte, 8)

		context.sender.SendPacket(dummyPacket)
	}
}
package rely

import (
	"github.com/op/go-logging"
	"testing"
)

func TestPacketHeader(t *testing.T) {
	logging.SetLevel(logging.ERROR, "rely")

	var writeSequence, writeAck, readSequence, readAck uint16
	var writeAckBits, readAckBits uint32

	packetData := newBuffer(MaxPacketHeaderBytes)

	// worst case, sequence and ack are far apart, no packets acked

	writeSequence = 10000
	writeAck = 100
	writeAckBits = 0

	bytesWritten := writePacketHeader(packetData, writeSequence, writeAck, writeAckBits)
	if bytesWritten != MaxPacketHeaderBytes {
		t.Error("Should have written", MaxPacketHeaderBytes, "but got", bytesWritten)
	}

	bytesRead := readPacketHeader("test_packet_header", packetData.buf, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// rare case. sequence and ack are far apart, significant # of acks are missing

	writeSequence = 10000
	writeAck = 100
	writeAckBits = 0xFEFEFFFE

	bytesWritten = writePacketHeader(packetData.reset(), writeSequence, writeAck, writeAckBits)
	if bytesWritten != 1+2+2+3 {
		t.Error(bytesWritten, "!=", 1+2+2+3)
	}

	bytesRead = readPacketHeader("test_packet_header", packetData.buf, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// common case under packet loss. sequence and ack are close together, some acks are missing

	writeSequence = 200
	writeAck = 100
	writeAckBits = 0xFFFEFFFF

	bytesWritten = writePacketHeader(packetData.reset(), writeSequence, writeAck, writeAckBits)

	if bytesWritten != 1+2+1+1 {
		t.Error(bytesWritten, "!=", 1+2+1+1)
	}

	bytesRead = readPacketHeader("test_packet_header", packetData.buf, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}

	// ideal case. no packet loss.

	writeSequence = 200
	writeAck = 100
	writeAckBits = 0xFFFFFFFF

	bytesWritten = writePacketHeader(packetData.reset(), writeSequence, writeAck, writeAckBits)

	if bytesWritten != 1+2+1 {
		t.Error(bytesWritten, "!=", 1+2+1)
	}

	bytesRead = readPacketHeader("test_packet_header", packetData.buf, &readSequence, &readAck, &readAckBits)
	if bytesRead != bytesWritten || readSequence != writeSequence || readAck != writeAck || readAckBits != writeAckBits {
		t.Error("read != write", bytesRead, bytesWritten, readSequence, writeSequence, readAck, writeAck, readAckBits, writeAckBits)
	}
}

type testContext struct {
	drop             int
	sender, receiver *Endpoint
}

func testTransmitPacketFunction(context interface{}, index int, sequence uint16, packetData []byte) {
	ctx := context.(*testContext)

	if ctx.drop > 0 {
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

	for i := 0; i < testAcksNumIterations; i++ {
		dummyPacket := []byte{1, 2, 3, 4, 5, 6, 7, 8}

		context.sender.SendPacket(dummyPacket)
		context.receiver.SendPacket(dummyPacket)

		context.sender.Update(time)
		context.receiver.Update(time)

		time += deltaTime
	}

	senderAckedPacket := make([]uint8, testAcksNumIterations)
	senderAcks := context.sender.GetAcks()
	for i := 0; i < len(senderAcks); i++ {
		if senderAcks[i] < testAcksNumIterations {
			senderAckedPacket[senderAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations/2; i++ {
		if senderAckedPacket[i] != 1 {
			t.Error("Packet not acked", i)
		}
	}

	receiverAckedPacket := make([]uint8, testAcksNumIterations)
	receiverAcks := context.receiver.GetAcks()
	for i := 0; i < len(receiverAcks); i++ {
		if receiverAcks[i] < testAcksNumIterations {
			receiverAckedPacket[receiverAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations/2; i++ {
		if receiverAckedPacket[i] != 1 {
			t.Fatal("Packet not acked", i)
		}
	}
}

func TestAcksPacketLoss(t *testing.T) {
	logging.SetLevel(logging.ERROR, "rely")

	time := 100.0

	context := testContext{}
	senderConfig := NewDefaultConfig()
	senderConfig.Name = "Sender"
	receiverConfig := NewDefaultConfig()
	receiverConfig.Name = "Receiver"

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

	deltaTime := 0.1
	for i := 0; i < testAcksNumIterations; i++ {
		dummyPacket := []uint8{1, 2, 3, 4, 5, 6, 7, 8}

		context.drop = i % 2

		context.sender.SendPacket(dummyPacket)
		context.receiver.SendPacket(dummyPacket)

		context.sender.Update(time)
		context.receiver.Update(time)

		time += deltaTime
	}

	senderAckedPacket := make([]uint8, testAcksNumIterations)
	senderAcks := context.sender.GetAcks()
	for i := 0; i < len(senderAcks); i++ {
		if senderAcks[i] < testAcksNumIterations {
			senderAckedPacket[senderAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations/2; i++ {
		if senderAckedPacket[i] != uint8((i+1)%2) {
			t.Fatal("Acked wrong at index", i, "should be", (i+1)%2, "but was", senderAckedPacket[i])
		}
	}

	receiverAckedPacket := make([]uint8, testAcksNumIterations)
	receiverAcks := context.sender.GetAcks()
	for i := 0; i < len(receiverAcks); i++ {
		if receiverAcks[i] < testAcksNumIterations {
			receiverAckedPacket[senderAcks[i]] = 1
		}
	}
	for i := 0; i < testAcksNumIterations/2; i++ {
		if receiverAckedPacket[i] != uint8((i+1)%2) {
			t.Fatal("Acked wrong at index", i, "should be", (i+1)%2, "but was", receiverAckedPacket[i])
		}
	}
}

const testMaxPacketBytes = 4 * 1024

func generatePacketData(sequence uint16) []byte {
	packetBytes := ((int(sequence) * 1023) % (testMaxPacketBytes - 2)) + 2
	if packetBytes < 2 || packetBytes > testMaxPacketBytes {
		log.Fatal("failed to gen packetBytes", packetBytes)
	}
	packetData := make([]byte, packetBytes)
	packetData[0] = byte(sequence & 0xFF)
	packetData[1] = byte((sequence >> 8) & 0xFF)
	for i := 2; i < packetBytes; i++ {
		packetData[i] = byte((i + int(sequence)) % 256)
	}
	return packetData
}

func testProcessPacketFunctionValidate(t *testing.T) func(context interface{}, index int, sequence uint16, packetData []byte) bool {
	return func(context interface{}, index int, sequence uint16, packetData []byte) bool {
		if packetData == nil || len(packetData) <= 0 || len(packetData) >= testMaxPacketBytes {
			t.Fatal("invalid packet data")
		}

		if len(packetData) < 2 {
			t.Fatal("invalid packet data size")
		}

		var seq uint16
		seq |= uint16(packetData[0])
		seq |= uint16(packetData[1]) << 8
		if len(packetData) < ((int(seq)*1023)%(testMaxPacketBytes-2))+2 {
			t.Fatal("Size not right, expected", ((int(seq)*1023)%(testMaxPacketBytes-2))+2, " got ", len(packetData))
		}
		for i := 2; i < len(packetData); i++ {
			if packetData[i] != byte((i+int(seq))%256) {
				t.Fatal("Wrong packet data at index", i, "got", packetData[i], "expected", (i+int(seq))%256)
			}
		}

		return true
	}
}

func TestPackets(t *testing.T) {
	//logging.SetLevel(logging.DEBUG, "rely")

	time := 100.

	context := testContext{}
	senderConfig := NewDefaultConfig()
	receiverConfig := NewDefaultConfig()

	senderConfig.FragmentAbove = 500
	receiverConfig.FragmentAbove = 500

	senderConfig.Context = &context
	senderConfig.Name = "sender"
	senderConfig.Index = 0
	senderConfig.TransmitPacketFunction = testTransmitPacketFunction
	senderConfig.ProcessPacketFunction = testProcessPacketFunctionValidate(t)

	receiverConfig.Context = &context
	receiverConfig.Name = "receiver"
	receiverConfig.Index = 1
	receiverConfig.TransmitPacketFunction = testTransmitPacketFunction
	receiverConfig.ProcessPacketFunction = testProcessPacketFunctionValidate(t)

	context.sender = NewEndpoint(senderConfig, time)
	context.receiver = NewEndpoint(receiverConfig, time)

	deltaTime := 0.1

	for i := 0; i < 16; i++ {
		{
			sequence := context.sender.NextPacketSequence()
			packetData := generatePacketData(sequence)
			context.sender.SendPacket(packetData)
		}

		{
			sequence := context.sender.NextPacketSequence()
			packetData := generatePacketData(sequence)
			context.sender.SendPacket(packetData)
		}

		context.sender.Update(time)
		context.receiver.Update(time)

		context.sender.ClearAcks()
		context.receiver.ClearAcks()

		time += deltaTime
	}
}

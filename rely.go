package rely

import (
	"unsafe"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("rely")

type FragmentReassemblyData struct {
	Sequence uint16
	Ack uint16
	AckBits uint32
	NumFragmentsReceived int
	NumFragmentsTotal int
	PacketData *Buffer
	PacketHeaderBytes int
	FragmentReceived [256]uint8
}

func (f *FragmentReassemblyData) StoreFragmentData(sequence, ack uint16, ackBits uint32, fragmentId, fragmentSize int, fragmentData *Buffer) {
	if fragmentId == 0 {
		packetHeader := NewBuffer(maxPacketHeaderBytes)
		f.PacketHeaderBytes = WritePacketHeader(packetHeader, sequence, ack, ackBits)
		// TODO not sure I got this right, I don't understand it yet
		b := NewBufferFromRef(f.PacketData.Buf[maxPacketHeaderBytes-f.PacketHeaderBytes:])
		b.WriteBytes(packetHeader.Buf)
		fragmentData += f.PacketHeaderBytes
		fragmentBytes -= f.PacketHeaderBytes
	}

	if fragmentId == f.NumFragmentsTotal - 1 {

	}

	// f.packetData[maxPacketHeaderBytes + fragmentId * fragmentSize] = fragmentData
}
func (f *FragmentReassemblyData) Cleanup() {}

type Endpoint struct {
	Config *Config
	Time float64
	rtt float64
	packetLoss float64
	SendBandwidthKbps float64
	ReceivedBandwidthKbps float64
	AckedBandwidthKbps float64
	NumAcks int
	Acks []uint16
	Sequence uint16
	SentPackets *SequenceBuffer
	ReceivedPackets *SequenceBuffer
	FragmentReassembly *SequenceBuffer
	Counters [counterMax]uint64
}

type SentPacketData struct {
	Time float64
	Acked uint32 // use only 1 bit
	PacketBytes uint32 // use only 31 bits
}

type ReceivedPacketData struct {
	Time float64
	PacketBytes uint32
}

func NewEndpoint(config *Config, time float64) *Endpoint {
	return &Endpoint{
		Config: config,
		Time: time,
		SentPackets: NewSequenceBuffer(config.SentPacketsBufferSize, int(unsafe.Sizeof(SentPacketData{}))),
		ReceivedPackets: NewSequenceBuffer(config.ReceivedPacketsBufferSize, int(unsafe.Sizeof(ReceivedPacketData{}))),
		FragmentReassembly: NewSequenceBuffer(config.FragmentReassemblyBufferSize, int(unsafe.Sizeof(FragmentReassemblyData{}))),
		Acks: make([]uint16, config.AckBufferSize),
	}
}

func (e *Endpoint) NextPacketSequence() uint16 {
	return e.Sequence
}

func (e *Endpoint) SendPacket(packetData *Buffer, packetBytes int) {
	if packetBytes > e.Config.MaxPacketSize {
		e.Counters[counterNumPacketsTooLargeToSend]++
		return
	}

	sequence := e.Sequence
	e.Sequence++
	var ack uint16
	var ackBits uint32

	e.ReceivedPackets.BufferGenerateAckBits(&ack, &ackBits)
	// TODO why does insert return void*? probably will find out later
	sentPacketData := e.SentPackets.Insert(sequence).(SentPacketData)
	sentPacketData.Time = e.Time
	sentPacketData.PacketBytes = uint32(e.Config.PacketHeaderSize + packetBytes)
	sentPacketData.Acked = 0

	if packetBytes <= e.Config.FragmentAbove {
		// regular packet
		log.Debug("[%s] sending packet %d without fragmentation\n", e.Config.Name, sequence)
		transmitPacketData := NewBuffer(packetBytes + maxPacketHeaderBytes)
		_ = WritePacketHeader(transmitPacketData, sequence, ack, ackBits)
		transmitPacketData.WriteBytes(packetData.Bytes())
		e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, transmitPacketData.Bytes())
		// TODO free(transmitPacketData)
	} else {
		// fragment packet
		packetHeader := NewBuffer(maxPacketHeaderBytes)
		_ = WritePacketHeader(packetHeader, sequence, ack, ackBits)
		var tmp int
		if packetBytes % e.Config.FragmentSize != 0 {
			tmp = 1
		}
		numFragments := (packetBytes / e.Config.FragmentSize) + tmp
		log.Debug("[%s] sending packet %d as %d fragments\n", e.Config.Name, sequence, numFragments)
		fragmentBufferSize := fragmentHeaderBytes + maxPacketHeaderBytes + e.Config.FragmentSize
		fragmentPacketData := NewBuffer(fragmentBufferSize)
		end := packetData.Pos + packetBytes
		for fragmentId := 0; fragmentId < numFragments; fragmentId++ {
			fragmentPacketData.WriteUint8(1)
			fragmentPacketData.WriteUint16(sequence)
			fragmentPacketData.WriteUint8(uint8(fragmentId))
			fragmentPacketData.WriteUint8(uint8(numFragments-1))

			if fragmentId == 0 {
				fragmentPacketData.WriteBytes(packetHeader.Bytes())
			}

			bytesToCopy := e.Config.FragmentSize
			if packetData.Pos + bytesToCopy > end {
				bytesToCopy = end - packetData.Pos
			}

			e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, fragmentPacketData.Bytes())
			e.Counters[counterNumFragmentsSent]++
		}
		// TODO free(fragmentPacketData)
	}
	e.Counters[counterNumPacketsSent]++
}

func (e *Endpoint) ReceivePacket() {}
func (e *Endpoint) FreePacket() {}
func (e *Endpoint) GetAcks() {}
func (e *Endpoint) ClearAcks() {}
func (e *Endpoint) Reset() {}
func (e *Endpoint) Update() {}
func (e *Endpoint) Rtt() {}
func (e *Endpoint) PacketLoss() {}
func (e *Endpoint) Bandwidth() {}

func WritePacketHeader(packetData *Buffer, sequence, ack uint16, ackBits uint32) int {
	var prefixByte uint8

	if (ackBits & 0x000000FF) != 0x000000FF {
		prefixByte |= 1<<1
	}

	if (ackBits & 0x0000FF00 ) != 0x0000FF00 {
		prefixByte |= 1<<2
	}

	if (ackBits & 0x00FF0000 ) != 0x00FF0000 {
		prefixByte |= 1<<3
	}

	if (ackBits & 0xFF000000 ) != 0xFF000000 {
		prefixByte |= 1<<4
	}

	seqDiff := sequence - ack
	if seqDiff < 0 {
		seqDiff += 65536
	}
	if seqDiff <= 255 {
		prefixByte |= 1<<5
	}

	start := packetData.Pos
	packetData.WriteUint8(prefixByte)
	packetData.WriteUint16(sequence)

	if seqDiff <= 255 {
		packetData.WriteUint8(uint8(seqDiff))
	} else {
		packetData.WriteUint16(ack)
	}

	if (ackBits & 0x000000FF) != 0x000000FF {
		packetData.WriteUint8(uint8(ackBits & 0x000000FF))
	}
	if (ackBits & 0x0000FF00) != 0x0000FF00 {
		packetData.WriteUint8(uint8(ackBits & 0x000000FF))
	}
	if (ackBits & 0x00FF0000) != 0x00FF0000 {
		packetData.WriteUint8(uint8(ackBits & 0x00FF0000))
	}
	if (ackBits & 0xFF000000) != 0xFF000000 {
		packetData.WriteUint8(uint8(ackBits & 0xFF000000))
	}

	return packetData.Pos - start
}

func ReadPacketHeader(name string, packetData []byte, sequence, ack *uint16, ackBits *uint32) int {
	packetBytes := len(packetData)
	if packetBytes < 3 {
		return -1
	}
	p := NewBufferFromRef(packetData)

	prefixByte, _ := p.GetUint8()

	if (prefixByte & 1) != 0 {
		log.Errorf("[%s] prefix byte does not indicate a regular packet\n", name)
		return -1
	}

	*sequence, _ = p.GetUint16()
	if prefixByte & (1<<5) == 1 {
		if packetBytes < 3+1 {
			log.Errorf("[%s] packet too small for packet header (2)\n", name)
			return -1
		}
		sequenceDifference, _ := p.GetUint8()
		*ack = *sequence - uint16(sequenceDifference)
	} else {
		if packetBytes < 3 + 2 {
			log.Errorf("[%s] packet too small for packet header (3)\n", name)
			return -1
		}
		*ack, _ = p.GetUint16()
	}

	var expectedBytes int
	var i uint
	for i = 1; i <= 4; i++ {
		if prefixByte & (1<<i) == 1 {
			expectedBytes++
		}
	}
	if packetBytes < (p.Pos - len(packetData)) + expectedBytes {
		log.Errorf("[%s] packet too small for packet header (4)\n", name)
		return -1
	}

	*ackBits = 0xFFFFFFFF
	if prefixByte & (1<<1) == 1 {
		*ackBits &= 0xFFFFFF00
		// TODO: this error stuff is getting old, maybe this copy of Buffer does less error checking
		b, _ := p.GetUint8()
		*ackBits |= uint32(b)
	}
	if prefixByte & (1<<2) == 1 {
		*ackBits &= 0xFFFF00FF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b<<8)
	}
	if prefixByte & (1<<3) == 1 {
		*ackBits &= 0xFF00FFFF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b<<16)
	}
	if prefixByte & (1<<4) == 1 {
		*ackBits &= 0x00FFFFFF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b<<24)
	}

	return int(p.Pos - len(packetData))
}

func ReadFragmentHeader(name string, packetData []byte, maxFragments, fragmentSize int, fragmentId, numFragments, fragmentBytes *int, sequence, ack *uint16, ackBits *uint32) int {
	packetBytes := len(packetData)
	if packetBytes < fragmentHeaderBytes {
		log.Errorf("[%s] packet is too small to read fragment header\n", name)
		return -1
	}

	p := NewBufferFromRef(packetData)
	prefixByte, _ := p.GetUint8()
	if prefixByte != 1 {
		log.Errorf("[%s] prefix byte is not a fragment\n", name)
		return -1
	}

	*sequence, _ = p.GetUint16()
	tmp, _ := p.GetUint8()
	*fragmentId = int(tmp)
	tmp, _ = p.GetUint8()
	*numFragments = int(tmp) + 1

	if *numFragments > maxFragments {
		log.Errorf("[%s] num fragments %d outside of range of max fragments %d\n", name, *numFragments, maxFragments)
		return -1
	}

	if *fragmentId >= *numFragments {
		log.Errorf( "[%s] fragment id %d outside of range of num fragments %d\n", name, *fragmentId, *numFragments)
		return -1
	}

	*fragmentBytes = packetBytes - fragmentHeaderBytes

	var packetSequence, packetAck uint16
	var packetAckBits uint32

	if *fragmentId == 0 {
		packetHeaderBytes := ReadPacketHeader(name, packetData[fragmentHeaderBytes:], &packetSequence, &packetAck, &packetAckBits)

		if packetHeaderBytes < 0 {
			log.Errorf( "[%s] bad packet header in fragment\n", name)
			return -1
		}

		if packetSequence != *sequence {
			log.Errorf( "[%s] bad packet sequence in fragment. expected %d, got %d\n", name, *sequence, packetSequence)
			return -1
		}

		*fragmentBytes = packetBytes - packetHeaderBytes - fragmentHeaderBytes
	}

	*ack = packetAck
	*ackBits = packetAckBits

	if *fragmentBytes > fragmentSize {
		log.Errorf( "[%s] fragment bytes %d > fragment size %d\n", name, *fragmentBytes, fragmentSize)
		return -1
	}

	if *fragmentId != *numFragments -1 && *fragmentBytes != fragmentSize {
		log.Errorf( "[%s] fragment %d is %d bytes, which is not the expected fragment size %d\n", name, *fragmentId, *fragmentBytes, fragmentSize)
		return -1
	}

	return p.Pos - len(packetData)
}

func LessThan(s1, s2 uint16) bool {
	return GreaterThan(s2, s1)
}

func GreaterThan(s1, s2 uint16) bool {
	return ( ( s1 > s2 ) && ( s1 - s2 <= 32768 ) ) || ( ( s1 < s2 ) && ( s2 - s1  > 32768 ) )
}

const (
	counterNumPacketsSent = iota
	counterNumPacketsReceived
	counterNumPacketsAcked
	counterNumPacketsStale
	counterNumPacketsInvalid
	counterNumPacketsTooLargeToSend
	counterNumPacketsTooLargeToReceive
	counterNumFragmentsSent
	counterNumFragmentsReceived
	counterNumFragmentsInvalid
	counterMax
)

const (
	maxPacketHeaderBytes = 9
	fragmentHeaderBytes = 5
)
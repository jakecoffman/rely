package rely

import (
	"unsafe"
	"github.com/op/go-logging"
	"math"
)

var log = logging.MustGetLogger("rely")

type Endpoint struct {
	Config                *Config
	Time                  float64
	rtt                   float64
	packetLoss            float64
	SentBandwidthKbps     float64
	ReceivedBandwidthKbps float64
	AckedBandwidthKbps    float64
	NumAcks               int
	Acks                  []uint16
	Sequence              uint16
	SentPackets           *SequenceBuffer
	ReceivedPackets       *SequenceBuffer
	FragmentReassembly    *SequenceBuffer
	Counters              [counterMax]uint64
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

func (e *Endpoint) SendPacket(packetData []byte) {
	packetBytes := len(packetData)
	if packetBytes > e.Config.MaxPacketSize {
		e.Counters[counterNumPacketsTooLargeToSend]++
		return
	}

	sequence := e.Sequence
	e.Sequence++
	var ack uint16
	var ackBits uint32

	e.ReceivedPackets.GenerateAckBits(&ack, &ackBits)
	sentPacketData := (*SentPacketData)(unsafe.Pointer(&e.SentPackets.Insert(sequence)[0]))
	sentPacketData.Time = e.Time
	sentPacketData.PacketBytes = uint32(e.Config.PacketHeaderSize + packetBytes)
	sentPacketData.Acked = 0

	if packetBytes <= e.Config.FragmentAbove {
		// regular packet
		log.Debug("[%s] sending packet %d without fragmentation\n", e.Config.Name, sequence)
		transmitPacketData := NewBuffer(packetBytes + maxPacketHeaderBytes)
		_ = WritePacketHeader(transmitPacketData.Buf, sequence, ack, ackBits)
		transmitPacketData.WriteBytes(packetData)
		e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, transmitPacketData.Bytes())
		// TODO free(transmitPacketData)
	} else {
		// fragment packet
		packetHeader := NewBuffer(maxPacketHeaderBytes)
		_ = WritePacketHeader(packetHeader.Buf, sequence, ack, ackBits)
		var extra int
		if packetBytes % e.Config.FragmentSize != 0 {
			extra = 1
		}
		numFragments := (packetBytes / e.Config.FragmentSize) + extra
		log.Debug("[%s] sending packet %d as %d fragments\n", e.Config.Name, sequence, numFragments)
		fragmentBufferSize := fragmentHeaderBytes + maxPacketHeaderBytes + e.Config.FragmentSize
		fragmentPacketData := make([]byte, fragmentBufferSize)

		q := NewBufferFromRef(packetData)
		p := NewBufferFromRef(fragmentPacketData)

		// write each fragment with header and data
		for fragmentId := 0; fragmentId < numFragments; fragmentId++ {
			p.Reset()
			p.WriteUint8(1)
			p.WriteUint16(sequence)
			p.WriteUint8(uint8(fragmentId))
			p.WriteUint8(uint8(numFragments-1))

			if fragmentId == 0 {
				p.WriteBytes(packetHeader.Bytes())
			}

			bytesToCopy := e.Config.FragmentSize
			if q.Pos + bytesToCopy > len(packetData) {
				bytesToCopy = len(packetData) - q.Pos
			}
			b, _ := q.GetBytes(bytesToCopy)
			p.WriteBytes(b)

			e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, p.Bytes())
			e.Counters[counterNumFragmentsSent]++
		}
		// TODO free(fragmentPacketData)
	}
	e.Counters[counterNumPacketsSent]++
}

func (e *Endpoint) ReceivePacket(packetData []byte) {
	if len(packetData) > e.Config.MaxPacketSize {
		log.Errorf("[%s] packet too large to receive. packet is %d bytes, maximum is %d\n", e.Config.Name, len(packetData), e.Config.MaxPacketSize)
		e.Counters[counterNumPacketsTooLargeToReceive]++
		return
	}

	prefixByte := packetData[0]
	if (prefixByte&1) == 0 {
		// normal packet
		e.Counters[counterNumPacketsReceived]++

		var sequence, ack uint16
		var ackBits uint32

		packetHeaderBytes := ReadPacketHeader(e.Config.Name, packetData, &sequence, &ack, &ackBits)
		if packetHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid packet. could not read packet header\n", e.Config.Name)
			e.Counters[counterNumPacketsInvalid]++
			return
		}

		if !e.ReceivedPackets.TestInsert(sequence) {
			log.Errorf("[%s] ignoring stale packet %d\n", e.Config.Name, sequence)
			e.Counters[counterNumPacketsStale]++
			return
		}

		log.Debugf("[%s] processing packet %d\n", e.Config.Name, sequence)
		if e.Config.ProcessPacketFunction(e.Config.Context, e.Config.Index, sequence, packetData[packetHeaderBytes:]) {
			log.Debugf("[%s] process packet %d successful\n", e.Config.Name, sequence)
			receivedPacketData := (*ReceivedPacketData)(unsafe.Pointer(&e.ReceivedPackets.Insert(sequence)[0]))
			receivedPacketData.Time = e.Time
			receivedPacketData.PacketBytes = uint32(e.Config.PacketHeaderSize + len(packetData))

			for i:=0; i<32; i++ {
				if ackBits & 1 != 0 {
					ackSequence := ack - uint16(i)
					sentPacketData := (*SentPacketData)(unsafe.Pointer(&e.SentPackets.Find(ackSequence)[0]))
					if sentPacketData != nil && sentPacketData.Acked != 0 && e.NumAcks < e.Config.AckBufferSize {
						log.Debugf("[%s] acked packet %d\n", e.Config.Name, sequence)
						e.Acks[e.NumAcks] = ackSequence
						e.NumAcks++
						e.Counters[counterNumPacketsAcked]++
						sentPacketData.Acked = 1

						rtt := float64(e.Time - sentPacketData.Time)*1000
						if e.rtt == 0 && rtt > 0 || math.Abs(e.rtt-rtt) < 0.00001 {
							e.rtt = rtt
						} else {
							e.rtt += (rtt - e.rtt) * e.Config.RttSmoothingFactor
						}
					}
				}
				ackBits >>= 1
			}
		} else {
			log.Errorf("[%s] process packet failed\n", e.Config.Name)
		}
	} else {
		// fragment packet
		var fragmentId, numFragments, fragmentBytes int
		var sequence, ack uint16
		var ackBits uint32

		fragHeaderBytes := ReadFragmentHeader(e.Config.Name, packetData, e.Config.MaxFragments, e.Config.FragmentSize, &fragmentId, &numFragments, &fragmentBytes, &sequence, &ack, &ackBits)
		if fragHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid fragment. could not read fragment header\n", e.Config.Name)
			e.Counters[counterNumFragmentsInvalid]++
			return
		}

		reassemblyData := (*FragmentReassemblyData)(unsafe.Pointer(&e.FragmentReassembly.Find(sequence)[0]))
		if reassemblyData == nil {
			// TODO withCleanup
			reassemblyData = (*FragmentReassemblyData)(unsafe.Pointer(&e.FragmentReassembly.Insert(sequence)[0]))
			if reassemblyData == nil {
				log.Errorf("[%s] ignoring invalid fragment. could not insert in reassembly buffer (stale)\n", e.Config.Name)
				e.Counters[counterNumFragmentsInvalid]++
				return
			}

			packetBufferSize := maxPacketHeaderBytes + numFragments*e.Config.FragmentSize
			reassemblyData.Sequence = sequence
			reassemblyData.Ack = 0
			reassemblyData.AckBits = 0
			reassemblyData.NumFragmentsReceived = 0
			reassemblyData.NumFragmentsTotal = numFragments
			reassemblyData.PacketData = make([]byte, packetBufferSize)
			reassemblyData.FragmentReceived = [256]uint8{}
		}

		if numFragments != int(reassemblyData.NumFragmentsTotal) {
			log.Errorf("[%s] ignoring invalid fragment. fragment count mismatch. expected %d, got %d\n", e.Config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			e.Counters[counterNumFragmentsInvalid]++
			return
		}

		if reassemblyData.FragmentReceived[fragmentId] != 0 {
			log.Errorf("[%s] ignoring fragment %d of packet %d. fragment already received\n", e.Config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			return
		}

		log.Debugf("[%s] received fragment %d of packet %d (%d/%d)\n", e.Config.Name, fragmentId, sequence, reassemblyData.NumFragmentsReceived+1, numFragments)
		reassemblyData.NumFragmentsReceived++
		reassemblyData.FragmentReceived[fragmentId] = 1
		reassemblyData.StoreFragmentData(sequence, ack, ackBits, fragmentId, e.Config.FragmentSize, packetData[fragHeaderBytes:])

		if reassemblyData.NumFragmentsReceived == reassemblyData.NumFragmentsTotal {
			log.Debugf("[%s] completed reassembly of packet %d\n", e.Config.Name, sequence)
			e.ReceivePacket(reassemblyData.PacketData[maxPacketHeaderBytes - reassemblyData.PacketHeaderBytes:])
			e.FragmentReassembly.Remove(sequence) // TODO withcleanup?
		}

		e.Counters[counterNumFragmentsReceived]++
	}
}

func (e *Endpoint) FreePacket() {}

func (e *Endpoint) GetAcks(numAcks *int) []uint16 {
	*numAcks = e.NumAcks
	return e.Acks
}

func (e *Endpoint) ClearAcks() {
	e.NumAcks = 0
	e.Acks = e.Acks[:]
}

func (e *Endpoint) Reset() {
	e.ClearAcks()
	e.Sequence = 0

	for i := 0; i<e.Config.FragmentReassemblyBufferSize; i++ {
		reassemblyData := (*FragmentReassemblyData)(unsafe.Pointer(&e.FragmentReassembly.AtIndex(i)[0]))

		if reassemblyData != nil && reassemblyData.PacketData != nil {
			reassemblyData.PacketData = nil
		}
	}

	e.SentPackets.Reset()
	e.ReceivedPackets.Reset()
	e.FragmentReassembly.Reset()
}

func (e *Endpoint) Update(time float64) {
	e.Time = time

	// calculate packet loss
	{
		baseSequence := (int(e.SentPackets.Sequence) - e.Config.SentPacketsBufferSize + 1) + 0xFFFF
		var numDropped int
		numSamples := e.Config.SentPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := uint16(baseSequence + i)
			sentPacketData := (*SentPacketData)(unsafe.Pointer(&e.SentPackets.Find(sequence)[0]))
			if sentPacketData != nil && sentPacketData.Acked == 0 {
				numDropped++
			}
		}
		packetLoss := float64(numDropped)/float64(numSamples) * 100
		if math.Abs(e.packetLoss - packetLoss) > 0.00001 {
			e.packetLoss += (packetLoss - e.packetLoss) * e.Config.PacketLossSmoothingFactor
		} else {
			e.packetLoss = packetLoss
		}
	}

	// calculate sent bandwidth
	{
		baseSequence := (int(e.SentPackets.Sequence) - e.Config.SentPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.Config.SentPacketsBufferSize/2
		for i := 0; i< numSamples; i++ {
			sequence := uint16(baseSequence + i)
			sentPacketData := (*SentPacketData)(unsafe.Pointer(&e.SentPackets.Find(sequence)[0]))
			if sentPacketData == nil {
				continue
			}
			bytesSent += int(sentPacketData.PacketBytes)
			if sentPacketData.Time < startTime {
				startTime = sentPacketData.Time
			}
			if sentPacketData.Time > finishTime {
				finishTime = sentPacketData.Time
			}
		}
		if startTime != math.MaxFloat64 && finishTime != 0 {
			sentBandwidthKbps := float64(bytesSent)/(finishTime - startTime) * 8/1000
			if math.Abs(sentBandwidthKbps - sentBandwidthKbps) > 0.00001 {
				e.SentBandwidthKbps += (sentBandwidthKbps - e.SentBandwidthKbps) * e.Config.BandwidthSmoothingFactor
			} else {
				e.SentBandwidthKbps = sentBandwidthKbps
			}
		}
	}

	// calculate received bandwidth
	{
		baseSequence := (int(e.ReceivedPackets.Sequence) - e.Config.ReceivedPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.Config.ReceivedPacketsBufferSize/2
		for i := 0; i<numSamples; i++ {
			sequence := uint16(baseSequence + i)
			receivedPacketData := (*ReceivedPacketData)(unsafe.Pointer(&e.ReceivedPackets.Find(sequence)[0]))
			if receivedPacketData == nil {
				continue
			}
			bytesSent += int(receivedPacketData.PacketBytes)
			if receivedPacketData.Time < startTime {
				startTime = receivedPacketData.Time
			}
			if receivedPacketData.Time > finishTime {
				finishTime = receivedPacketData.Time
			}
		}
		if startTime != math.MaxFloat64 && finishTime != 0 {
			receivedBandwidthKbps := float64(bytesSent)/(finishTime - startTime)*8/1000
			if math.Abs(e.ReceivedBandwidthKbps - receivedBandwidthKbps) > 0.00001 {
				e.ReceivedBandwidthKbps += (receivedBandwidthKbps - e.ReceivedBandwidthKbps) * e.Config.BandwidthSmoothingFactor
			} else {
				e.ReceivedBandwidthKbps = receivedBandwidthKbps
			}
		}
	}

	// calculate acked bandwidth
	{
		baseSequence := (int(e.SentPackets.Sequence) - e.Config.SentPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.Config.ReceivedPacketsBufferSize/2
		for i := 0; i < numSamples; i++ {
			sequence := uint16(baseSequence + i)
			sentPacketData := (*SentPacketData)(unsafe.Pointer(&e.SentPackets.Find(sequence)[0]))
			if sentPacketData == nil || sentPacketData.Acked == 0 {
				continue
			}
			bytesSent += int(sentPacketData.PacketBytes)
			if sentPacketData.Time < startTime {
				startTime = sentPacketData.Time
			}
			if sentPacketData.Time > finishTime {
				finishTime = sentPacketData.Time
			}
		}
		if startTime != math.MaxFloat64 && finishTime != 0 {
			ackedBandwidthKbps := float64(bytesSent)/(finishTime - startTime) * 8/1000
			if math.Abs(e.AckedBandwidthKbps - ackedBandwidthKbps) > 0.00001 {
				e.AckedBandwidthKbps += (ackedBandwidthKbps - e.AckedBandwidthKbps) * e.Config.BandwidthSmoothingFactor
			} else {
				e.AckedBandwidthKbps = ackedBandwidthKbps
			}
		}
	}
}

func (e *Endpoint) Rtt() float64 {
	return e.rtt
}

func (e *Endpoint) PacketLoss() float64 {
	return e.packetLoss
}

func (e *Endpoint) Bandwidth() (float64, float64, float64) {
	return e.SentBandwidthKbps, e.ReceivedBandwidthKbps, e.AckedBandwidthKbps
}

func WritePacketHeader(packetData []byte, sequence, ack uint16, ackBits uint32) int {
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

	seqDiff := int(sequence - ack)
	if seqDiff < 0 {
		seqDiff += 65536
	}
	if seqDiff <= 255 {
		prefixByte |= 1<<5
	}

	p := NewBufferFromRef(packetData)
	p.WriteUint8(prefixByte)
	p.WriteUint16(sequence)

	if seqDiff <= 255 {
		p.WriteUint8(uint8(seqDiff))
	} else {
		p.WriteUint16(ack)
	}

	if (ackBits & 0x000000FF) != 0x000000FF {
		p.WriteUint8(uint8(ackBits & 0x000000FF))
	}
	if (ackBits & 0x0000FF00) != 0x0000FF00 {
		p.WriteUint8(uint8(ackBits & 0x000000FF >> 8))
	}
	if (ackBits & 0x00FF0000) != 0x00FF0000 {
		p.WriteUint8(uint8(ackBits & 0x00FF0000 >> 16))
	}
	if (ackBits & 0xFF000000) != 0xFF000000 {
		p.WriteUint8(uint8(ackBits & 0xFF000000 >> 24))
	}

	return p.Pos
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
	if prefixByte & (1<<5) != 0 {
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
		if prefixByte & (1<<i) != 0 {
			expectedBytes++
		}
	}
	if packetBytes < p.Pos + expectedBytes {
		log.Errorf("[%s] packet too small for packet header (4)\n", name)
		return -1
	}

	*ackBits = 0xFFFFFFFF
	if prefixByte & (1<<1) != 0 {
		*ackBits &= 0xFFFFFF00
		// TODO: this error stuff is getting old, maybe this copy of Buffer does less error checking
		b, _ := p.GetUint8()
		*ackBits |= uint32(b)
	}
	if prefixByte & (1<<2) != 0 {
		*ackBits &= 0xFFFF00FF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b) << 8
	}
	if prefixByte & (1<<3) != 0 {
		*ackBits &= 0xFF00FFFF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b) << 16
	}
	if prefixByte & (1<<4) != 0 {
		*ackBits &= 0x00FFFFFF
		b, _ := p.GetUint8()
		*ackBits |= uint32(b) << 24
	}

	return p.Pos
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
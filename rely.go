package rely

import (
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
	SentPackets           *sentPacketSequenceBuffer
	ReceivedPackets       *receivedPacketSequenceBuffer
	FragmentReassembly    *fragmentSequenceBuffer
	Counters              [CounterMax]uint64
}

func NewEndpoint(config *Config, time float64) *Endpoint {
	return &Endpoint{
		Config:             config,
		Time:               time,
		SentPackets:        newSentPacketSequenceBuffer(config.SentPacketsBufferSize),
		ReceivedPackets:    newReceivedPacketSequenceBuffer(config.ReceivedPacketsBufferSize),
		FragmentReassembly: newFragmentSequenceBuffer(config.FragmentReassemblyBufferSize),
		Acks:               make([]uint16, config.AckBufferSize),
	}
}

func (e *Endpoint) NextPacketSequence() uint16 {
	return e.Sequence
}

func (e *Endpoint) SendPacket(packetData []byte) {
	packetBytes := len(packetData)
	if packetBytes > e.Config.MaxPacketSize {
		e.Counters[CounterNumPacketsTooLargeToSend]++
		return
	}

	sequence := e.Sequence
	e.Sequence++
	var ack uint16
	var ackBits uint32

	e.ReceivedPackets.GenerateAckBits(&ack, &ackBits)
	sentPacketData := e.SentPackets.Insert(sequence)
	sentPacketData.Time = e.Time
	sentPacketData.PacketBytes = uint32(e.Config.PacketHeaderSize + packetBytes)
	sentPacketData.Acked = 0

	if packetBytes <= e.Config.FragmentAbove {
		// regular packet
		log.Debugf("[%s] sending packet %d without fragmentation", e.Config.Name, sequence)
		transmitPacketData := newBuffer(packetBytes + MaxPacketHeaderBytes)
		_ = WritePacketHeader(transmitPacketData, sequence, ack, ackBits)
		transmitPacketData.writeBytes(packetData)
		e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, transmitPacketData.bytes())
		// TODO free(transmitPacketData)
	} else {
		// fragment packet
		packetHeader := newBuffer(MaxPacketHeaderBytes)
		_ = WritePacketHeader(packetHeader, sequence, ack, ackBits)
		var extra int
		if packetBytes % e.Config.FragmentSize != 0 {
			extra = 1
		}
		numFragments := (packetBytes / e.Config.FragmentSize) + extra
		log.Debugf("[%s] sending packet %d as %d fragments", e.Config.Name, sequence, numFragments)
		fragmentBufferSize := FragmentHeaderBytes + MaxPacketHeaderBytes + e.Config.FragmentSize

		q := newBufferFromRef(packetData)
		p := newBuffer(fragmentBufferSize)

		// write each fragment with header and data
		for fragmentId := 0; fragmentId < numFragments; fragmentId++ {
			p.reset()
			p.writeUint8(1)
			p.writeUint16(sequence)
			p.writeUint8(uint8(fragmentId))
			p.writeUint8(uint8(numFragments-1))

			if fragmentId == 0 {
				p.writeBytes(packetHeader.bytes())
			}

			bytesToCopy := e.Config.FragmentSize
			if q.pos+ bytesToCopy > len(packetData) {
				bytesToCopy = len(packetData) - q.pos
			}
			b, _ := q.getBytes(bytesToCopy)
			p.writeBytes(b)

			e.Config.TransmitPacketFunction(e.Config.Context, e.Config.Index, sequence, p.bytes())
			e.Counters[CounterNumFragmentsSent]++
		}
		// TODO free(fragmentPacketData)
	}
	e.Counters[CounterNumPacketsSent]++
}

func (e *Endpoint) ReceivePacket(packetData []byte) {
	if len(packetData) > e.Config.MaxPacketSize {
		log.Errorf("[%s] packet too large to receive. packet is %d bytes, maximum is %d", e.Config.Name, len(packetData), e.Config.MaxPacketSize)
		e.Counters[CounterNumPacketsTooLargeToReceive]++
		return
	}

	prefixByte := packetData[0]
	if (prefixByte&1) == 0 {
		// normal packet
		e.Counters[CounterNumPacketsReceived]++

		var sequence, ack uint16
		var ackBits uint32

		packetHeaderBytes := ReadPacketHeader(e.Config.Name, packetData, &sequence, &ack, &ackBits)
		if packetHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid packet. could not read packet header", e.Config.Name)
			e.Counters[CounterNumPacketsInvalid]++
			return
		}

		if !e.ReceivedPackets.TestInsert(sequence) {
			log.Errorf("[%s] ignoring stale packet %d", e.Config.Name, sequence)
			e.Counters[CounterNumPacketsStale]++
			return
		}

		log.Debugf("[%s] processing packet %d", e.Config.Name, sequence)
		if e.Config.ProcessPacketFunction(e.Config.Context, e.Config.Index, sequence, packetData[packetHeaderBytes:]) {
			log.Debugf("[%s] process packet %d successful", e.Config.Name, sequence)
			receivedPacketData := e.ReceivedPackets.Insert(sequence)
			receivedPacketData.Time = e.Time
			receivedPacketData.PacketBytes = uint32(e.Config.PacketHeaderSize + len(packetData))

			for i := 0; i < 32; i++ {
				if ackBits & 1 != 0 {
					ackSequence := ack - uint16(i)
					sentPacketData := e.SentPackets.Find(ackSequence)
					if sentPacketData != nil && sentPacketData.Acked == 0 && e.NumAcks < e.Config.AckBufferSize {
						log.Debugf("[%s] acked packet %d", e.Config.Name, sequence)
						e.Acks[e.NumAcks] = ackSequence
						e.NumAcks++
						e.Counters[CounterNumPacketsAcked]++
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
		}
	} else {
		// fragment packet
		var fragmentId, numFragments, fragmentBytes int
		var sequence, ack uint16
		var ackBits uint32

		fragHeaderBytes := ReadFragmentHeader(e.Config.Name, packetData, e.Config.MaxFragments, e.Config.FragmentSize, &fragmentId, &numFragments, &fragmentBytes, &sequence, &ack, &ackBits)
		if fragHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid fragment. could not read fragment header", e.Config.Name)
			e.Counters[CounterNumFragmentsInvalid]++
			return
		}

		reassemblyData := e.FragmentReassembly.Find(sequence)
		if reassemblyData == nil {
			// TODO withCleanup
			reassemblyData = e.FragmentReassembly.Insert(sequence)
			if reassemblyData == nil {
				log.Errorf("[%s] ignoring invalid fragment. could not insert in reassembly buffer (stale)", e.Config.Name)
				e.Counters[CounterNumFragmentsInvalid]++
				return
			}

			packetBufferSize := MaxPacketHeaderBytes + numFragments*e.Config.FragmentSize
			reassemblyData.Sequence = sequence
			reassemblyData.Ack = 0
			reassemblyData.AckBits = 0
			reassemblyData.NumFragmentsReceived = 0
			reassemblyData.NumFragmentsTotal = numFragments
			reassemblyData.PacketData = make([]byte, packetBufferSize)
			reassemblyData.FragmentReceived = [256]uint8{}
		}

		if numFragments != int(reassemblyData.NumFragmentsTotal) {
			log.Errorf("[%s] ignoring invalid fragment. fragment count mismatch. expected %d, got %d", e.Config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			e.Counters[CounterNumFragmentsInvalid]++
			return
		}

		if reassemblyData.FragmentReceived[fragmentId] != 0 {
			log.Errorf("[%s] ignoring fragment %d of packet %d. fragment already received", e.Config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			return
		}

		log.Debugf("[%s] received fragment %d of packet %d (%d/%d)", e.Config.Name, fragmentId, sequence, reassemblyData.NumFragmentsReceived+1, numFragments)
		reassemblyData.NumFragmentsReceived++
		reassemblyData.FragmentReceived[fragmentId] = 1
		reassemblyData.StoreFragmentData(sequence, ack, ackBits, fragmentId, e.Config.FragmentSize, packetData[fragHeaderBytes:])

		if reassemblyData.NumFragmentsReceived == reassemblyData.NumFragmentsTotal {
			log.Debugf("[%s] completed reassembly of packet %d", e.Config.Name, sequence)
			e.ReceivePacket(reassemblyData.PacketData[MaxPacketHeaderBytes - reassemblyData.PacketHeaderBytes:MaxPacketHeaderBytes + reassemblyData.PacketBytes])
			e.FragmentReassembly.Remove(sequence) // TODO withcleanup?
		}

		e.Counters[CounterNumFragmentsReceived]++
	}
}

func (e *Endpoint) FreePacket() {}

func (e *Endpoint) GetAcks() (int, []uint16) {
	return e.NumAcks, e.Acks
}

func (e *Endpoint) ClearAcks() {
	e.NumAcks = 0
	e.Acks = e.Acks[:]
}

func (e *Endpoint) Reset() {
	e.ClearAcks()
	e.Sequence = 0

	for i := 0; i<e.Config.FragmentReassemblyBufferSize; i++ {
		reassemblyData := e.FragmentReassembly.AtIndex(i)

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
		baseSequence := (e.SentPackets.Sequence - uint16(e.Config.SentPacketsBufferSize) + 1) + 0xFFFF
		var numDropped int
		numSamples := e.Config.SentPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := baseSequence + uint16(i)
			sentPacketData := e.SentPackets.Find(sequence)
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
			sentPacketData := e.SentPackets.Find(sequence)
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
			receivedPacketData := e.ReceivedPackets.Find(sequence)
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
			sentPacketData := e.SentPackets.Find(sequence)
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

func WritePacketHeader(packetData *buffer, sequence, ack uint16, ackBits uint32) int {
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

	packetData.writeUint8(prefixByte)
	packetData.writeUint16(sequence)

	if seqDiff <= 255 {
		packetData.writeUint8(uint8(seqDiff))
	} else {
		packetData.writeUint16(ack)
	}

	if (ackBits & 0x000000FF) != 0x000000FF {
		packetData.writeUint8(uint8(ackBits & 0x000000FF))
	}
	if (ackBits & 0x0000FF00) != 0x0000FF00 {
		packetData.writeUint8(uint8(ackBits & 0x000000FF >> 8))
	}
	if (ackBits & 0x00FF0000) != 0x00FF0000 {
		packetData.writeUint8(uint8(ackBits & 0x00FF0000 >> 16))
	}
	if (ackBits & 0xFF000000) != 0xFF000000 {
		packetData.writeUint8(uint8(ackBits & 0xFF000000 >> 24))
	}

	return packetData.pos
}

func ReadPacketHeader(name string, packetData []byte, sequence, ack *uint16, ackBits *uint32) int {
	packetBytes := len(packetData)
	if packetBytes < 3 {
		return -1
	}
	p := newBufferFromRef(packetData)

	prefixByte, _ := p.getUint8()

	if (prefixByte & 1) != 0 {
		log.Errorf("[%s] prefix byte does not indicate a regular packet", name)
		return -1
	}

	*sequence, _ = p.getUint16()
	if prefixByte & (1<<5) != 0 {
		if packetBytes < 3+1 {
			log.Errorf("[%s] packet too small for packet header (2)", name)
			return -1
		}
		sequenceDifference, _ := p.getUint8()
		*ack = *sequence - uint16(sequenceDifference)
	} else {
		if packetBytes < 3 + 2 {
			log.Errorf("[%s] packet too small for packet header (3)", name)
			return -1
		}
		*ack, _ = p.getUint16()
	}

	var expectedBytes int
	var i uint
	for i = 1; i <= 4; i++ {
		if prefixByte & (1<<i) != 0 {
			expectedBytes++
		}
	}
	if packetBytes < p.pos+ expectedBytes {
		log.Errorf("[%s] packet too small for packet header (4)", name)
		return -1
	}

	*ackBits = 0xFFFFFFFF
	if prefixByte & (1<<1) != 0 {
		*ackBits &= 0xFFFFFF00
		b, _ := p.getUint8()
		*ackBits |= uint32(b)
	}
	if prefixByte & (1<<2) != 0 {
		*ackBits &= 0xFFFF00FF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 8
	}
	if prefixByte & (1<<3) != 0 {
		*ackBits &= 0xFF00FFFF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 16
	}
	if prefixByte & (1<<4) != 0 {
		*ackBits &= 0x00FFFFFF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 24
	}

	return p.pos
}

func ReadFragmentHeader(name string, packetData []byte, maxFragments, fragmentSize int, fragmentId, numFragments, fragmentBytes *int, sequence, ack *uint16, ackBits *uint32) int {
	packetBytes := len(packetData)
	if packetBytes < FragmentHeaderBytes {
		log.Errorf("[%s] packet is too small to read fragment header", name)
		return -1
	}

	p := newBufferFromRef(packetData)
	prefixByte, _ := p.getUint8()
	if prefixByte != 1 {
		log.Errorf("[%s] prefix byte is not a fragment", name)
		return -1
	}

	*sequence, _ = p.getUint16()
	tmp, _ := p.getUint8()
	*fragmentId = int(tmp)
	tmp, _ = p.getUint8()
	*numFragments = int(tmp) + 1

	if *numFragments > maxFragments {
		log.Errorf("[%s] num fragments %d outside of range of max fragments %d", name, *numFragments, maxFragments)
		return -1
	}

	if *fragmentId >= *numFragments {
		log.Errorf( "[%s] fragment id %d outside of range of num fragments %d", name, *fragmentId, *numFragments)
		return -1
	}

	*fragmentBytes = packetBytes - FragmentHeaderBytes

	var packetSequence, packetAck uint16
	var packetAckBits uint32

	if *fragmentId == 0 {
		packetHeaderBytes := ReadPacketHeader(name, packetData[FragmentHeaderBytes:], &packetSequence, &packetAck, &packetAckBits)

		if packetHeaderBytes < 0 {
			log.Errorf( "[%s] bad packet header in fragment", name)
			return -1
		}

		if packetSequence != *sequence {
			log.Errorf( "[%s] bad packet sequence in fragment. expected %d, got %d", name, *sequence, packetSequence)
			return -1
		}

		*fragmentBytes = packetBytes - packetHeaderBytes - FragmentHeaderBytes
	}

	*ack = packetAck
	*ackBits = packetAckBits

	if *fragmentBytes > fragmentSize {
		log.Errorf( "[%s] fragment bytes %d > fragment size %d", name, *fragmentBytes, fragmentSize)
		return -1
	}

	if *fragmentId != *numFragments - 1 && *fragmentBytes != fragmentSize {
		log.Errorf( "[%s] fragment %d is %d bytes, which is not the expected fragment size %d", name, *fragmentId, *fragmentBytes, fragmentSize)
		return -1
	}

	return p.pos
}

func LessThan(s1, s2 uint16) bool {
	return GreaterThan(s2, s1)
}

func GreaterThan(s1, s2 uint16) bool {
	return ( ( s1 > s2 ) && ( s1 - s2 <= 32768 ) ) || ( ( s1 < s2 ) && ( s2 - s1  > 32768 ) )
}

const (
	CounterNumPacketsSent                                          = iota
	CounterNumPacketsReceived
	CounterNumPacketsAcked
	CounterNumPacketsStale
	CounterNumPacketsInvalid
	CounterNumPacketsTooLargeToSend
	CounterNumPacketsTooLargeToReceive
	CounterNumFragmentsSent
	CounterNumFragmentsReceived
	CounterNumFragmentsInvalid
	CounterMax
)

const (
	MaxPacketHeaderBytes = 9
	FragmentHeaderBytes  = 5
)

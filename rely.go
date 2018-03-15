// This is a port of reliable.io to Go. See also https://gafferongames.com/post/reliable_ordered_messages/
package rely

import (
	"github.com/op/go-logging"
	"math"
)

var log = logging.MustGetLogger("rely")

// Endpoint is a reliable udp endpoint
type Endpoint struct {
	config                *Config
	time                  float64
	rtt                   float64
	packetLoss            float64
	sentBandwidthKbps     float64
	receivedBandwidthKbps float64
	ackedBandwidthKbps    float64
	numAcks               int
	acks                  []uint16
	sequence              uint16
	sentPackets           *sentPacketSequenceBuffer
	receivedPackets       *receivedPacketSequenceBuffer
	fragmentReassembly    *fragmentSequenceBuffer
	counters              [counterMax]uint64

	allocate func(int) []byte
	free     func([]byte)
}

// NewEndpoint creates an endpoint
func NewEndpoint(config *Config, time float64) *Endpoint {
	endpoint := &Endpoint{
		config:             config,
		time:               time,
		sentPackets:        newSentPacketSequenceBuffer(config.SentPacketsBufferSize),
		receivedPackets:    newReceivedPacketSequenceBuffer(config.ReceivedPacketsBufferSize),
		fragmentReassembly: newFragmentSequenceBuffer(config.FragmentReassemblyBufferSize),
		acks:               make([]uint16, config.AckBufferSize),
		allocate:           config.Allocate,
		free:               config.Free,
	}
	if endpoint.allocate == nil {
		endpoint.allocate = defaultAllocate
	}
	if endpoint.free == nil {
		endpoint.free = defaultFree
	}

	return endpoint
}

func defaultAllocate(size int) []byte {
	return make([]byte, size)
}
func defaultFree(_ []byte) {}

// NextPacketSequence returns the next packet sequence that will be used
func (e *Endpoint) NextPacketSequence() uint16 {
	return e.sequence
}

// SendPacket reliably sends one or more packets with the passed
func (e *Endpoint) SendPacket(packetData []byte) {
	packetBytes := len(packetData)
	if packetBytes > e.config.MaxPacketSize {
		e.counters[counterNumPacketsTooLargeToSend]++
		return
	}

	sequence := e.sequence
	e.sequence++
	var ack uint16
	var ackBits uint32

	e.receivedPackets.GenerateAckBits(&ack, &ackBits)
	sentPacketData := e.sentPackets.Insert(sequence)
	sentPacketData.Time = e.time
	sentPacketData.PacketBytes = uint32(e.config.PacketHeaderSize + packetBytes)
	sentPacketData.Acked = 0

	if packetBytes <= e.config.FragmentAbove {
		// regular packet
		log.Debugf("[%s] sending packet %d without fragmentation", e.config.Name, sequence)
		transmitPacketData := newBufferFromRef(e.allocate(packetBytes + MaxPacketHeaderBytes))
		_ = writePacketHeader(transmitPacketData, sequence, ack, ackBits)
		transmitPacketData.writeBytes(packetData)
		e.config.TransmitPacketFunction(e.config.Context, e.config.Index, sequence, transmitPacketData.bytes())
		e.free(transmitPacketData.buf)
	} else {
		// fragment packet
		packetHeader := newBufferFromRef(e.allocate(MaxPacketHeaderBytes))
		_ = writePacketHeader(packetHeader, sequence, ack, ackBits)
		var extra int
		if packetBytes%e.config.FragmentSize != 0 {
			extra = 1
		}
		numFragments := (packetBytes / e.config.FragmentSize) + extra
		log.Debugf("[%s] sending packet %d as %d fragments", e.config.Name, sequence, numFragments)
		fragmentBufferSize := FragmentHeaderBytes + MaxPacketHeaderBytes + e.config.FragmentSize

		q := newBufferFromRef(packetData)
		p := newBufferFromRef(e.allocate(fragmentBufferSize))

		// write each fragment with header and data
		for fragmentId := 0; fragmentId < numFragments; fragmentId++ {
			p.reset()
			p.writeUint8(1)
			p.writeUint16(sequence)
			p.writeUint8(uint8(fragmentId))
			p.writeUint8(uint8(numFragments - 1))

			if fragmentId == 0 {
				p.writeBytes(packetHeader.bytes())
			}

			bytesToCopy := e.config.FragmentSize
			if q.pos+bytesToCopy > len(packetData) {
				bytesToCopy = len(packetData) - q.pos
			}
			b, _ := q.getBytes(bytesToCopy)
			p.writeBytes(b)

			e.config.TransmitPacketFunction(e.config.Context, e.config.Index, sequence, p.bytes())
			e.counters[counterNumFragmentsSent]++
		}
		e.free(p.buf)
		e.free(packetHeader.buf)
	}
	e.counters[counterNumPacketsSent]++
}

// ReceivePacket reliably receives a packet of data sent by SendPacket
func (e *Endpoint) ReceivePacket(packetData []byte) {
	if len(packetData) > e.config.MaxPacketSize {
		log.Errorf("[%s] packet too large to receive. packet is %d bytes, maximum is %d", e.config.Name, len(packetData), e.config.MaxPacketSize)
		e.counters[counterNumPacketsTooLargeToReceive]++
		return
	}

	prefixByte := packetData[0]
	if (prefixByte & 1) == 0 {
		// normal packet
		e.counters[counterNumPacketsReceived]++

		var sequence, ack uint16
		var ackBits uint32

		packetHeaderBytes := readPacketHeader(e.config.Name, packetData, &sequence, &ack, &ackBits)
		if packetHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid packet. could not read packet header", e.config.Name)
			e.counters[counterNumPacketsInvalid]++
			return
		}

		if !e.receivedPackets.TestInsert(sequence) {
			log.Errorf("[%s] ignoring stale packet %d", e.config.Name, sequence)
			e.counters[counterNumPacketsStale]++
			return
		}

		log.Debugf("[%s] processing packet %d", e.config.Name, sequence)
		if e.config.ProcessPacketFunction(e.config.Context, e.config.Index, sequence, packetData[packetHeaderBytes:]) {
			log.Debugf("[%s] process packet %d successful", e.config.Name, sequence)
			receivedPacketData := e.receivedPackets.Insert(sequence)
			receivedPacketData.Time = e.time
			receivedPacketData.PacketBytes = uint32(e.config.PacketHeaderSize + len(packetData))

			for i := 0; i < 32; i++ {
				if ackBits&1 != 0 {
					ackSequence := ack - uint16(i)
					sentPacketData := e.sentPackets.Find(ackSequence)
					if sentPacketData != nil && sentPacketData.Acked == 0 && e.numAcks < e.config.AckBufferSize {
						log.Debugf("[%s] acked packet %d", e.config.Name, sequence)
						e.acks[e.numAcks] = ackSequence
						e.numAcks++
						e.counters[counterNumPacketsAcked]++
						sentPacketData.Acked = 1

						rtt := float64(e.time-sentPacketData.Time) * 1000
						if e.rtt == 0 && rtt > 0 || math.Abs(e.rtt-rtt) < 0.00001 {
							e.rtt = rtt
						} else {
							e.rtt += (rtt - e.rtt) * e.config.RttSmoothingFactor
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

		fragHeaderBytes := readFragmentHeader(e.config.Name, packetData, e.config.MaxFragments, e.config.FragmentSize, &fragmentId, &numFragments, &fragmentBytes, &sequence, &ack, &ackBits)
		if fragHeaderBytes < 0 {
			log.Errorf("[%s] ignoring invalid fragment. could not read fragment header", e.config.Name)
			e.counters[counterNumFragmentsInvalid]++
			return
		}

		reassemblyData := e.fragmentReassembly.Find(sequence)
		if reassemblyData == nil {
			reassemblyData = e.fragmentReassembly.Insert(sequence)
			if reassemblyData == nil {
				log.Errorf("[%s] ignoring invalid fragment. could not insert in reassembly buffer (stale)", e.config.Name)
				e.counters[counterNumFragmentsInvalid]++
				return
			}

			packetBufferSize := MaxPacketHeaderBytes + numFragments*e.config.FragmentSize
			reassemblyData.Sequence = sequence
			reassemblyData.Ack = 0
			reassemblyData.AckBits = 0
			reassemblyData.NumFragmentsReceived = 0
			reassemblyData.NumFragmentsTotal = numFragments
			reassemblyData.PacketData = e.allocate(packetBufferSize)
			reassemblyData.FragmentReceived = [256]uint8{}
		}

		if numFragments != int(reassemblyData.NumFragmentsTotal) {
			log.Errorf("[%s] ignoring invalid fragment. fragment count mismatch. expected %d, got %d", e.config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			e.counters[counterNumFragmentsInvalid]++
			return
		}

		if reassemblyData.FragmentReceived[fragmentId] != 0 {
			log.Errorf("[%s] ignoring fragment %d of packet %d. fragment already received", e.config.Name, reassemblyData.NumFragmentsTotal, numFragments)
			return
		}

		log.Debugf("[%s] received fragment %d of packet %d (%d/%d)", e.config.Name, fragmentId, sequence, reassemblyData.NumFragmentsReceived+1, numFragments)
		reassemblyData.NumFragmentsReceived++
		reassemblyData.FragmentReceived[fragmentId] = 1
		reassemblyData.StoreFragmentData(sequence, ack, ackBits, fragmentId, e.config.FragmentSize, packetData[fragHeaderBytes:])

		if reassemblyData.NumFragmentsReceived == reassemblyData.NumFragmentsTotal {
			log.Debugf("[%s] completed reassembly of packet %d", e.config.Name, sequence)
			e.ReceivePacket(reassemblyData.PacketData[MaxPacketHeaderBytes-reassemblyData.PacketHeaderBytes:MaxPacketHeaderBytes+reassemblyData.PacketBytes])
			e.free(reassemblyData.PacketData)
			e.fragmentReassembly.Remove(sequence)
		}

		e.counters[counterNumFragmentsReceived]++
	}
}

// GetAcks returns the number of acks and the acks themselves
func (e *Endpoint) GetAcks() (int, []uint16) {
	return e.numAcks, e.acks
}

// ClearAcks clears the endpoints ack array
func (e *Endpoint) ClearAcks() {
	e.numAcks = 0
	e.acks = e.acks[:]
}

// Reset starts the endpoint fresh
func (e *Endpoint) Reset() {
	e.ClearAcks()
	e.sequence = 0

	for i := 0; i < e.config.FragmentReassemblyBufferSize; i++ {
		reassemblyData := e.fragmentReassembly.AtIndex(i)

		if reassemblyData != nil && reassemblyData.PacketData != nil {
			reassemblyData.PacketData = nil
		}
	}

	e.sentPackets.Reset()
	e.receivedPackets.Reset()
	e.fragmentReassembly.Reset()
}

// Update recalculates statistics (like packet loss)
func (e *Endpoint) Update(time float64) {
	e.time = time

	// calculate packet loss
	{
		baseSequence := (e.sentPackets.Sequence - uint16(e.config.SentPacketsBufferSize) + 1) + 0xFFFF
		var numDropped int
		numSamples := e.config.SentPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := baseSequence + uint16(i)
			sentPacketData := e.sentPackets.Find(sequence)
			if sentPacketData != nil && sentPacketData.Acked == 0 {
				numDropped++
			}
		}
		packetLoss := float64(numDropped) / float64(numSamples) * 100
		if math.Abs(e.packetLoss-packetLoss) > 0.00001 {
			e.packetLoss += (packetLoss - e.packetLoss) * e.config.PacketLossSmoothingFactor
		} else {
			e.packetLoss = packetLoss
		}
	}

	// calculate sent bandwidth
	{
		baseSequence := (int(e.sentPackets.Sequence) - e.config.SentPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.config.SentPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := uint16(baseSequence + i)
			sentPacketData := e.sentPackets.Find(sequence)
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
			sentBandwidthKbps := float64(bytesSent) / (finishTime - startTime) * 8 / 1000
			if math.Abs(sentBandwidthKbps-sentBandwidthKbps) > 0.00001 {
				e.sentBandwidthKbps += (sentBandwidthKbps - e.sentBandwidthKbps) * e.config.BandwidthSmoothingFactor
			} else {
				e.sentBandwidthKbps = sentBandwidthKbps
			}
		}
	}

	// calculate received bandwidth
	{
		baseSequence := (int(e.receivedPackets.Sequence) - e.config.ReceivedPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.config.ReceivedPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := uint16(baseSequence + i)
			receivedPacketData := e.receivedPackets.Find(sequence)
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
			receivedBandwidthKbps := float64(bytesSent) / (finishTime - startTime) * 8 / 1000
			if math.Abs(e.receivedBandwidthKbps-receivedBandwidthKbps) > 0.00001 {
				e.receivedBandwidthKbps += (receivedBandwidthKbps - e.receivedBandwidthKbps) * e.config.BandwidthSmoothingFactor
			} else {
				e.receivedBandwidthKbps = receivedBandwidthKbps
			}
		}
	}

	// calculate acked bandwidth
	{
		baseSequence := (int(e.sentPackets.Sequence) - e.config.SentPacketsBufferSize + 1) + 0xFFFF
		var bytesSent int
		startTime := math.MaxFloat64
		var finishTime float64
		numSamples := e.config.ReceivedPacketsBufferSize / 2
		for i := 0; i < numSamples; i++ {
			sequence := uint16(baseSequence + i)
			sentPacketData := e.sentPackets.Find(sequence)
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
			ackedBandwidthKbps := float64(bytesSent) / (finishTime - startTime) * 8 / 1000
			if math.Abs(e.ackedBandwidthKbps-ackedBandwidthKbps) > 0.00001 {
				e.ackedBandwidthKbps += (ackedBandwidthKbps - e.ackedBandwidthKbps) * e.config.BandwidthSmoothingFactor
			} else {
				e.ackedBandwidthKbps = ackedBandwidthKbps
			}
		}
	}
}

// PacketsSent returns the number of packets sent
func (e *Endpoint) PacketsSent() uint64 {
	return e.counters[counterNumPacketsSent]
}

// PacketsReceived returns the number of packets received
func (e *Endpoint) PacketsReceived() uint64 {
	return e.counters[counterNumPacketsReceived]
}

// PacketsAcked returns the number of packets acked
func (e *Endpoint) PacketsAcked() uint64 {
	return e.counters[counterNumPacketsAcked]
}

// Rtt returns the round-trip time
func (e *Endpoint) Rtt() float64 {
	return e.rtt
}

// PacketLoss returns the percent of packets lost this endpoint is experiencing
func (e *Endpoint) PacketLoss() float64 {
	return e.packetLoss
}

// Bandwith returns the sent, received, and acked bandwidth in Kbps
func (e *Endpoint) Bandwidth() (float64, float64, float64) {
	return e.sentBandwidthKbps, e.receivedBandwidthKbps, e.ackedBandwidthKbps
}

func writePacketHeader(packetData *buffer, sequence, ack uint16, ackBits uint32) int {
	var prefixByte uint8

	if (ackBits & 0x000000FF) != 0x000000FF {
		prefixByte |= 1 << 1
	}

	if (ackBits & 0x0000FF00 ) != 0x0000FF00 {
		prefixByte |= 1 << 2
	}

	if (ackBits & 0x00FF0000 ) != 0x00FF0000 {
		prefixByte |= 1 << 3
	}

	if (ackBits & 0xFF000000 ) != 0xFF000000 {
		prefixByte |= 1 << 4
	}

	seqDiff := int(sequence - ack)
	if seqDiff < 0 {
		seqDiff += 65536
	}
	if seqDiff <= 255 {
		prefixByte |= 1 << 5
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

func readPacketHeader(name string, packetData []byte, sequence, ack *uint16, ackBits *uint32) int {
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
	if prefixByte&(1<<5) != 0 {
		if packetBytes < 3+1 {
			log.Errorf("[%s] packet too small for packet header (2)", name)
			return -1
		}
		sequenceDifference, _ := p.getUint8()
		*ack = *sequence - uint16(sequenceDifference)
	} else {
		if packetBytes < 3+2 {
			log.Errorf("[%s] packet too small for packet header (3)", name)
			return -1
		}
		*ack, _ = p.getUint16()
	}

	var expectedBytes int
	var i uint
	for i = 1; i <= 4; i++ {
		if prefixByte&(1<<i) != 0 {
			expectedBytes++
		}
	}
	if packetBytes < p.pos+expectedBytes {
		log.Errorf("[%s] packet too small for packet header (4)", name)
		return -1
	}

	*ackBits = 0xFFFFFFFF
	if prefixByte&(1<<1) != 0 {
		*ackBits &= 0xFFFFFF00
		b, _ := p.getUint8()
		*ackBits |= uint32(b)
	}
	if prefixByte&(1<<2) != 0 {
		*ackBits &= 0xFFFF00FF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 8
	}
	if prefixByte&(1<<3) != 0 {
		*ackBits &= 0xFF00FFFF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 16
	}
	if prefixByte&(1<<4) != 0 {
		*ackBits &= 0x00FFFFFF
		b, _ := p.getUint8()
		*ackBits |= uint32(b) << 24
	}

	return p.pos
}

func readFragmentHeader(name string, packetData []byte, maxFragments, fragmentSize int, fragmentId, numFragments, fragmentBytes *int, sequence, ack *uint16, ackBits *uint32) int {
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
		log.Errorf("[%s] fragment id %d outside of range of num fragments %d", name, *fragmentId, *numFragments)
		return -1
	}

	*fragmentBytes = packetBytes - FragmentHeaderBytes

	var packetSequence, packetAck uint16
	var packetAckBits uint32

	if *fragmentId == 0 {
		packetHeaderBytes := readPacketHeader(name, packetData[FragmentHeaderBytes:], &packetSequence, &packetAck, &packetAckBits)

		if packetHeaderBytes < 0 {
			log.Errorf("[%s] bad packet header in fragment", name)
			return -1
		}

		if packetSequence != *sequence {
			log.Errorf("[%s] bad packet sequence in fragment. expected %d, got %d", name, *sequence, packetSequence)
			return -1
		}

		*fragmentBytes = packetBytes - packetHeaderBytes - FragmentHeaderBytes
	}

	*ack = packetAck
	*ackBits = packetAckBits

	if *fragmentBytes > fragmentSize {
		log.Errorf("[%s] fragment bytes %d > fragment size %d", name, *fragmentBytes, fragmentSize)
		return -1
	}

	if *fragmentId != *numFragments-1 && *fragmentBytes != fragmentSize {
		log.Errorf("[%s] fragment %d is %d bytes, which is not the expected fragment size %d", name, *fragmentId, *fragmentBytes, fragmentSize)
		return -1
	}

	return p.pos
}

func lessThan(s1, s2 uint16) bool {
	return greaterThan(s2, s1)
}

func greaterThan(s1, s2 uint16) bool {
	return ( ( s1 > s2 ) && ( s1-s2 <= 32768 ) ) || ( ( s1 < s2 ) && ( s2-s1 > 32768 ) )
}

const (
	counterNumPacketsSent              = iota
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
	MaxPacketHeaderBytes = 9
	FragmentHeaderBytes  = 5
)

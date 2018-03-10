package rely

import (
	"math"
)

// TODO add option for custom allocator, especially to avoid GC

// sequenceBuffer is a generic store for sent and received packets, as well as fragments of packets.
// The entry data is the actual custom packet data that the user is trying to send
type sequenceBuffer struct {
	Sequence uint16
	NumEntries int
	EntrySequence []uint32
}

const available = 0xFFFFFFFF

// newSequenceBuffer creates a sequence buffer with specified entries and stride (size of each packet's data)
func newSequenceBuffer(numEntries int) *sequenceBuffer {
	sb :=  &sequenceBuffer{
		NumEntries: numEntries,
		EntrySequence: make([]uint32, numEntries),
	}
	sb.Reset()
	return sb
}

// Reset starts the sequence buffer from scratch
func (sb *sequenceBuffer) Reset() {
	sb.Sequence = 0
	for i := 0; i < sb.NumEntries; i++ {
		sb.EntrySequence[i] = math.MaxUint32
	}
}

// RemoveEntries removes old entries from start sequence to finish sequence (inclusive)
func (sb *sequenceBuffer) RemoveEntries(start, finish int) {
	if finish < start {
		finish += 65535
	}
	if finish - start < sb.NumEntries {
		for sequence := start; sequence <= finish; sequence++ {
			// cleanup?
			sb.EntrySequence[sequence%sb.NumEntries] = available
		}
	} else {
		for i := 0; i < sb.NumEntries; i++ {
			sb.EntrySequence[i] = available
		}
	}
}

// TestInsert checks to see if the sequence can be inserted
func (sb *sequenceBuffer) TestInsert(sequence uint16) bool {
	if lessThan(sequence, sb.Sequence - uint16(sb.NumEntries)) {
		return false
	}
	return true
}

// TODO
func (sb *sequenceBuffer) InsertWithCleanup() interface{} {
	panic("TODO")
	return nil
}

func (sb *sequenceBuffer) Remove(sequence uint16) {
	sb.EntrySequence[int(sequence)%sb.NumEntries] = available
}

// TODO
func (sb *sequenceBuffer) RemoveWithCleanup() {}

func (sb *sequenceBuffer) Available(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)] == available
}

func (sb *sequenceBuffer) Exists(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)%sb.NumEntries] == uint32(sequence)
}

func (sb *sequenceBuffer) GenerateAckBits(ack *uint16, ackBits *uint32) {
	*ack = sb.Sequence-1
	*ackBits = 0
	var mask uint32 = 1
	for i:=0; i<32; i++ {
		sequence := *ack - uint16(i)
		if sb.Exists(sequence) {
			*ackBits |= mask
		}
		mask <<= 1
	}
}


type sentPacketSequenceBuffer struct {
	*sequenceBuffer
	EntryData []sentPacketData
}

func newSentPacketSequenceBuffer(numEntries int) *sentPacketSequenceBuffer {
	return &sentPacketSequenceBuffer{
		sequenceBuffer: newSequenceBuffer(numEntries),
		EntryData:      make([]sentPacketData, numEntries),
	}
}

// Insert marks the sequence as used and returns an address to the buffer, or nil if insertion is invalid
func (sb *sentPacketSequenceBuffer) Insert(sequence uint16) *sentPacketData {
	if lessThan(sequence, sb.Sequence-uint16(sb.NumEntries)) {
		// sequence is too low
		return nil
	}
	if greaterThan(sequence + 1, sb.Sequence) {
		// move the sequence forward, drop old entries
		sb.RemoveEntries(int(sb.Sequence), int(sequence))
		sb.Sequence = sequence + 1
	}
	index := int(sequence) % sb.NumEntries
	sb.EntrySequence[index] = uint32(sequence)
	return &sb.EntryData[index]
}

// Find returns the entry data for the sequence, or nil if there is none
func (sb *sentPacketSequenceBuffer) Find(sequence uint16) *sentPacketData {
	index := int(sequence) % sb.NumEntries
	if sb.EntrySequence[index] == uint32(sequence) {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}

func (sb *sentPacketSequenceBuffer) AtIndex(index int) *sentPacketData {
	if sb.EntrySequence[index] != available {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}

type receivedPacketSequenceBuffer struct {
	*sequenceBuffer
	EntryData []receivedPacketData
}

func newReceivedPacketSequenceBuffer(numEntries int) *receivedPacketSequenceBuffer {
	return &receivedPacketSequenceBuffer{
		sequenceBuffer: newSequenceBuffer(numEntries),
		EntryData:      make([]receivedPacketData, numEntries),
	}
}

// Insert marks the sequence as used and returns an address to the buffer, or nil if insertion is invalid
func (sb *receivedPacketSequenceBuffer) Insert(sequence uint16) *receivedPacketData {
	if lessThan(sequence, sb.Sequence-uint16(sb.NumEntries)) {
		// sequence is too low
		return nil
	}
	if greaterThan(sequence + 1, sb.Sequence) {
		// move the sequence forward, drop old entries
		sb.RemoveEntries(int(sb.Sequence), int(sequence))
		sb.Sequence = sequence + 1
	}
	index := int(sequence) % sb.NumEntries
	sb.EntrySequence[index] = uint32(sequence)
	return &sb.EntryData[index]
}

// Find returns the entry data for the sequence, or nil if there is none
func (sb *receivedPacketSequenceBuffer) Find(sequence uint16) *receivedPacketData {
	index := int(sequence) % sb.NumEntries
	if sb.EntrySequence[index] == uint32(sequence) {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}

func (sb *receivedPacketSequenceBuffer) AtIndex(index int) *receivedPacketData {
	if sb.EntrySequence[index] != available {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}

type fragmentSequenceBuffer struct {
	*sequenceBuffer
	EntryData []fragmentReassemblyData
}

func newFragmentSequenceBuffer(numEntries int) *fragmentSequenceBuffer {
	return &fragmentSequenceBuffer{
		sequenceBuffer: newSequenceBuffer(numEntries),
		EntryData:      make([]fragmentReassemblyData, numEntries),
	}
}

// Insert marks the sequence as used and returns an address to the buffer, or nil if insertion is invalid
func (sb *fragmentSequenceBuffer) Insert(sequence uint16) *fragmentReassemblyData {
	if lessThan(sequence, sb.Sequence-uint16(sb.NumEntries)) {
		// sequence is too low
		return nil
	}
	if greaterThan(sequence + 1, sb.Sequence) {
		// move the sequence forward, drop old entries
		sb.RemoveEntries(int(sb.Sequence), int(sequence))
		sb.Sequence = sequence + 1
	}
	index := int(sequence) % sb.NumEntries
	sb.EntrySequence[index] = uint32(sequence)
	return &sb.EntryData[index]
}

// Find returns the entry data for the sequence, or nil if there is none
func (sb *fragmentSequenceBuffer) Find(sequence uint16) *fragmentReassemblyData {
	index := int(sequence) % sb.NumEntries
	if sb.EntrySequence[index] == uint32(sequence) {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}

func (sb *fragmentSequenceBuffer) AtIndex(index int) *fragmentReassemblyData {
	if sb.EntrySequence[index] != available {
		return &sb.EntryData[index]
	} else {
		return nil
	}
}


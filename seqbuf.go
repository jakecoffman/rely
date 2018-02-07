package rely

import (
	"math"
)

// TODO add option for custom allocator, especially to avoid GC
// TODO make this less generic to avoid unsafe.Pointer calls: implement for each packet type in this package

// SequenceBuffer is a generic store for sent and received packets, as well as fragments of packets.
// The entry data is the actual custom packet data that the user is trying to send
type SequenceBuffer struct {
	Sequence uint16
	NumEntries int
	EntryStride int
	EntrySequence []uint32
	EntryData []byte
}

const available = 0xFFFFFFFF

// NewSequenceBuffer creates a sequence buffer with specified entries and stride (size of each packet's data)
func NewSequenceBuffer(numEntries, entryStride int) *SequenceBuffer {
	sb :=  &SequenceBuffer{
		NumEntries: numEntries,
		EntryStride: entryStride,
		EntrySequence: make([]uint32, numEntries),
		EntryData: make([]byte, numEntries*entryStride),
	}
	sb.Reset()
	return sb
}

// Reset starts the sequence buffer from scratch
func (sb *SequenceBuffer) Reset() {
	sb.Sequence = 0
	for i := 0; i < sb.NumEntries; i++ {
		sb.EntrySequence[i] = math.MaxUint32
	}
}

// RemoveEntries removes old entries from start sequence to finish sequence (inclusive)
func (sb *SequenceBuffer) RemoveEntries(start, finish int) {
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
func (sb *SequenceBuffer) TestInsert(sequence uint16) bool {
	if LessThan(sequence, sb.Sequence - uint16(sb.NumEntries)) {
		return false
	}
	return true
}

// Insert marks the sequence as used and returns an address to the buffer, or nil if insertion is invalid
func (sb *SequenceBuffer) Insert(sequence uint16) []byte {
	if LessThan(sequence, sb.Sequence-uint16(sb.NumEntries)) {
		// sequence is too low
		return nil
	}
	if GreaterThan(sequence + 1, sb.Sequence) {
		// move the sequence forward, drop old entries
		sb.RemoveEntries(int(sb.Sequence), int(sequence))
		sb.Sequence = sequence + 1
	}
	index := int(sequence) % sb.NumEntries
	sb.EntrySequence[index] = uint32(sequence)
	return sb.EntryData[index*sb.EntryStride:index*sb.EntryStride+sb.EntryStride]
}

// TODO
func (sb *SequenceBuffer) InsertWithCleanup() interface{} {
	panic("TODO")
	return nil
}

func (sb *SequenceBuffer) Remove(sequence uint16) {
	sb.EntrySequence[int(sequence)%sb.NumEntries] = available
}

// TODO
func (sb *SequenceBuffer) RemoveWithCleanup() {}

func (sb *SequenceBuffer) Available(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)] == available
}

func (sb *SequenceBuffer) Exists(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)%sb.NumEntries] == uint32(sequence)
}

// Find returns the entry data for the sequence, or nil if there is none
func (sb *SequenceBuffer) Find(sequence uint16) []byte {
	index := int(sequence) % sb.NumEntries
	if sb.EntrySequence[index] == uint32(sequence) {
		location := index*sb.EntryStride
		return sb.EntryData[location:location+sb.EntryStride]
	} else {
		return nil
	}
}

func (sb *SequenceBuffer) AtIndex(index int) []byte {
	if sb.EntrySequence[index] != available {
		location := index*sb.EntryStride
		return sb.EntryData[location:location+sb.EntryStride]
	} else {
		return nil
	}
}

func (sb *SequenceBuffer) GenerateAckBits(ack *uint16, ackBits *uint32) {
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

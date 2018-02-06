package rely

// TODO add option for custom allocator, especially to avoid GC
type SequenceBuffer struct {
	Sequence uint16
	NumEntries int
	EntryStride int
	EntrySequence []uint32
	EntryData []uint8
}

const available = 0xFFFFFFFF

func NewSequenceBuffer(numEntries, entryStride int) *SequenceBuffer {
	sb :=  &SequenceBuffer{
		NumEntries: numEntries,
		EntryStride: entryStride,
		EntrySequence: make([]uint32, numEntries),
		EntryData: make([]uint8, numEntries*entryStride),
	}
	sb.Reset()
	return sb
}

func (sb *SequenceBuffer) Reset() {
	sb.Sequence = 0
	for i := range sb.EntrySequence {
		sb.EntrySequence[i] = 0xFF
	}
}

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
func (sb *SequenceBuffer) TestInsert(sequence uint16) int {
	if LessThan(sequence, sb.Sequence - uint16(sb.NumEntries)) {
		return 0
	}
	return 1
}
func (sb *SequenceBuffer) Insert(sequence uint16) interface{} {
	if LessThan(sequence, sb.Sequence-uint16(sb.NumEntries)) {
		return nil
	}
	if GreaterThan(sequence + 1, sb.Sequence) {
		sb.RemoveEntries(int(sb.Sequence), int(sequence))
		sb.Sequence = sequence + 1
	}
	index := int(sequence) % sb.NumEntries
	sb.EntrySequence[index] = uint32(sequence)
	return sb.EntryData[index*sb.EntryStride]
}
func (sb *SequenceBuffer) InsertWithCleanup() {}
func (sb *SequenceBuffer) Remove(sequence uint16) {
	sb.EntrySequence[int(sequence)%sb.NumEntries] = available
}
func (sb *SequenceBuffer) RemoveWithCleanup() {}
func (sb *SequenceBuffer) BufferAvailable(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)] == available
}
func (sb *SequenceBuffer) BufferExists(sequence uint16) bool {
	return sb.EntrySequence[int(sequence)%sb.NumEntries] == uint32(sequence)
}
func (sb *SequenceBuffer) BufferFind(sequence uint16) interface{} {
	index := int(sequence) % sb.NumEntries
	if sb.EntrySequence[index] == uint32(sequence) {
		return sb.EntryData[index*sb.EntryStride]
	} else {
		return nil
	}
}
func (sb *SequenceBuffer) BufferAtIndex(index int) interface{} {
	if sb.EntrySequence[index] != available {
		return sb.EntryData[index*sb.EntryStride]
	} else {
		return nil
	}
}
func (sb *SequenceBuffer) BufferGenerateAckBits(ack *uint16, ackBits *uint32) {
	*ack = sb.Sequence-1
	*ackBits = 0
	var mask uint32 = 1
	for i:=0; i<32; i++ {
		sequence := *ack - uint16(i)
		if sb.BufferExists(sequence) {
			*ackBits |= mask
		}
		mask <<= 1
	}
}

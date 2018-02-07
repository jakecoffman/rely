package rely

import (
	"testing"
	"unsafe"
)

type testSequenceData struct {
	sequence uint16
}

const testSequenceBufferSize = 256

func TestSequenceBuffer_Find(t *testing.T) {
	sb := NewSequenceBuffer(testSequenceBufferSize, int(unsafe.Sizeof(testSequenceData{})))
	if sb.Sequence != 0 || sb.NumEntries != testSequenceBufferSize || sb.EntryStride != 2 {
		t.Error("Failed to construct:", sb.Sequence, sb.NumEntries, sb.EntryStride)
	}

	for i := 0; i < testSequenceBufferSize; i++ {
		v := sb.Find(uint16(i))
		if v != nil {
			t.Error("At index", i, "got bytes", v, "but expected nil")
		}
	}

	for i := 0; i <= testSequenceBufferSize*4; i++ {
		entryBytes := sb.Insert(uint16(i))
		if entryBytes == nil {
			t.Error("Failed to insert entry")
		}
		entry := (*testSequenceData)(unsafe.Pointer(&entryBytes[0]))
		entry.sequence = uint16(i)
		if int(sb.Sequence) != i+1 {
			t.Error("Should be", i+1, "but was", sb.Sequence)
		}
	}

	for i := 0; i <= testSequenceBufferSize; i++ {
		entryBytes := sb.Insert(uint16(i))
		if entryBytes != nil {
			t.Error("Should not have been nil", i)
		}
	}

	index := testSequenceBufferSize * 4
	for i := 0; i< testSequenceBufferSize; i++ {
		entryBytes := sb.Find(uint16(index))
		if entryBytes == nil {
			t.Error("Shouldn't have been nil", i)
		}
		entry := (*testSequenceData)(unsafe.Pointer(&entryBytes[0]))
		if entry.sequence != uint16(index) {
			t.Error("Entry", i, "at index", index, "not equal", entry.sequence)
		}
		index--
	}

	sb.Reset()

	for i := 0; i < testSequenceBufferSize; i++ {
		if sb.Find(uint16(i)) != nil {
			t.Error("Index not reset:", i)
		}
	}
}

func TestSequenceBuffer_GenerateAckBits(t *testing.T) {
	sb := NewSequenceBuffer(testSequenceBufferSize, int(unsafe.Sizeof(testSequenceData{})))

	var ack uint16 = 0
	var ackBits uint32 = 0xFFFFFFFF

	sb.GenerateAckBits(&ack, &ackBits)
	if ack != 0xFFFF || ackBits != 0 {
		t.Error("failed to generate ack bits", ack, ackBits)
	}

	for i := 0; i <= testSequenceBufferSize; i++ {
		sb.Insert(uint16(i))
	}

	sb.GenerateAckBits(&ack, &ackBits)
	if ack != testSequenceBufferSize || ackBits != 0xFFFFFFFF {
		t.Error("Failed to generate ack bits", ack, ackBits)
	}

	sb.Reset()
	inputAcks := []uint16{1, 5, 9, 11}
	for _, v := range inputAcks {
		sb.Insert(v)
	}

	sb.GenerateAckBits(&ack, &ackBits)

	if ack != 11 || ackBits != ( 1 | (1<<(11-9)) | (1<<(11-5)) | (1<<(11-1)) ) {
		t.Error("Failed to generate ack bits", ack, ackBits)
	}
}

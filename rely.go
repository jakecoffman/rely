package rely

import (
	"fmt"
	"unsafe"
)

type Config struct {
	Name    string
	Context interface{}
	Index int
	MaxPacketSize int
	FragmentAbove int
	MaxFragments int
	FragmentSize int
	AckBufferSize int
	SentPacketsBufferSize int
	ReceivedPacketsBufferSize int
	FragmentReassemblyBufferSize int
	RttSmoothingFactor float64
	PacketLossSmoothingFactor float64
	BandwidthSmoothingFactor float64
	PacketHeaderSize int
	TransmitPacketFunction func(interface{}, int, uint16, uint8, int)
	ProcessPacketFunction func(interface{}, int, uint16, uint8, int)
	AllocatorContext interface{}
	AllocateFunction func(interface{}, uint64)
	FreeFunction func(interface{}, interface{})
}

func NewDefaultConfig() *Config {
	return &Config{
		Name: "endpoint",
		MaxPacketSize: 16*1024,
		FragmentAbove: 1024,
		MaxFragments: 16,
		FragmentSize: 1024,
		AckBufferSize: 256,
		SentPacketsBufferSize: 256,
		ReceivedPacketsBufferSize: 256,
		FragmentReassemblyBufferSize: 64,
		RttSmoothingFactor: .0025,
		PacketLossSmoothingFactor: .1,
		BandwidthSmoothingFactor: .1,
		PacketHeaderSize: 28, // // note: UDP over IPv4 = 20 + 8 bytes, UDP over IPv6 = 40 + 8 bytes
	}
}

// TODO add option for custom allocator, especially to avoid GC
type SequenceBuffer struct {
	Sequence uint16
	NumEntries int
	EntryStride int
	EntrySequence []uint32
	EntryData []uint8
}

func NewSequenceBuffer(numEntries, entryStride int, context interface{}) *SequenceBuffer {
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
			sb.EntrySequence[sequence%sb.NumEntries] = 0xFFFFFFFF
		}
	} else {
		for i := 0; i < sb.NumEntries; i++ {
			sb.EntrySequence[i] = 0xFFFFFFFF
		}
	}
}

func (sb *SequenceBuffer) TestInsert(sequence uint16) int {
	if sb.LessThan(sequence, sb.Sequence - uint16(sb.NumEntries)) {
		return 0
	}
	return 1
}

func (sb *SequenceBuffer) Insert() {}
func (sb *SequenceBuffer) InsertWithCleanup() {}
func (sb *SequenceBuffer) Remove() {}
func (sb *SequenceBuffer) RemoveWithCleanup() {}
func (sb *SequenceBuffer) BufferAvailable() {}
func (sb *SequenceBuffer) BufferExists() {}
func (sb *SequenceBuffer) BufferFind() {}
func (sb *SequenceBuffer) BufferAtIndex() {}
func (sb *SequenceBuffer) BufferGenerateAckBits() {}

type FragmentReassemblyData struct {

}

func (f *FragmentReassemblyData) StoreFragmentData() {}
func (f *FragmentReassemblyData) Cleanup() {}

type Endpoint struct {

}

type SentPacketData struct {

}

type ReceivedPacketData struct {

}

func NewEndpoint() *Endpoint {
	return &Endpoint{}
}

func (e *Endpoint) Destroy() {}
func (e *Endpoint) NextPacketSequence() {}
func (e *Endpoint) SendPacket() {}
func (e *Endpoint) ReceivePacket() {}
func (e *Endpoint) FreePacket() {}
func (e *Endpoint) GetAcks() {}
func (e *Endpoint) ClearAcks() {}
func (e *Endpoint) Reset() {}
func (e *Endpoint) Update() {}
func (e *Endpoint) Rtt() {}
func (e *Endpoint) PacketLoss() {}
func (e *Endpoint) Bandwidth() {}

func WritePacketHeader() {}
func ReadPacketHeader() {}
func ReadFragmentHeader() {}

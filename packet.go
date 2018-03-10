package rely

type SentPacketData struct {
	Time float64
	Acked uint32 // use only 1 bit
	PacketBytes uint32 // use only 31 bits
}

type ReceivedPacketData struct {
	Time float64
	PacketBytes uint32
}

type FragmentReassemblyData struct {
	Sequence uint16
	Ack uint16
	AckBits uint32
	NumFragmentsReceived int
	NumFragmentsTotal int
	PacketData []byte
	PacketBytes int
	PacketHeaderBytes int
	FragmentReceived [256]uint8
}

func (f *FragmentReassemblyData) StoreFragmentData(sequence, ack uint16, ackBits uint32, fragmentId, fragmentSize int, fragmentData []byte) {
	// if this is the first fragment, write the header and advance the fragmentData cursor
	if fragmentId == 0 {
		packetHeader := NewBuffer(MaxPacketHeaderBytes)
		f.PacketHeaderBytes = WritePacketHeader(packetHeader, sequence, ack, ackBits)
		// leaves a gap at the front of the buffer?
		copy(f.PacketData[MaxPacketHeaderBytes-f.PacketHeaderBytes:], packetHeader.Bytes())
		fragmentData = fragmentData[f.PacketHeaderBytes:]
	}

	// if this is the last fragment, we know the final size of the packet
	if fragmentId == f.NumFragmentsTotal - 1 {
		f.PacketBytes = (f.NumFragmentsTotal-1) * fragmentSize + len(fragmentData)
	}

	// copy the fragment data into the right spot in the array
	copy(f.PacketData[MaxPacketHeaderBytes+fragmentId*fragmentSize:], fragmentData)
}

func (f *FragmentReassemblyData) Cleanup() {}

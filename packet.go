package rely

type sentPacketData struct {
	Time float64
	Acked uint32 // use only 1 bit
	PacketBytes uint32 // use only 31 bits
}

type receivedPacketData struct {
	Time float64
	PacketBytes uint32
}

type fragmentReassemblyData struct {
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

func (f *fragmentReassemblyData) StoreFragmentData(sequence, ack uint16, ackBits uint32, fragmentId, fragmentSize int, fragmentData []byte) {
	// if this is the first fragment, write the header and advance the fragmentData cursor
	if fragmentId == 0 {
		packetHeader := newBuffer(MaxPacketHeaderBytes)
		f.PacketHeaderBytes = writePacketHeader(packetHeader, sequence, ack, ackBits)
		// leaves a gap at the front of the buffer?
		copy(f.PacketData[MaxPacketHeaderBytes-f.PacketHeaderBytes:], packetHeader.bytes())
		fragmentData = fragmentData[f.PacketHeaderBytes:]
	}

	// if this is the last fragment, we know the final size of the packet
	if fragmentId == f.NumFragmentsTotal - 1 {
		f.PacketBytes = (f.NumFragmentsTotal-1) * fragmentSize + len(fragmentData)
	}

	// copy the fragment data into the right spot in the array
	copy(f.PacketData[MaxPacketHeaderBytes+fragmentId*fragmentSize:], fragmentData)
}

func (f *fragmentReassemblyData) Cleanup() {}

package rely

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
	TransmitPacketFunction func(interface{}, int, uint16, []byte)
	ProcessPacketFunction func(interface{}, int, uint16, []byte)
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

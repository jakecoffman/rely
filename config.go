package rely

// Config holds endpoint configuration data
type Config struct {
	Name                         string
	Context                      interface{}
	Index                        int
	MaxPacketSize                int
	FragmentAbove                int
	MaxFragments                 int
	FragmentSize                 int
	AckBufferSize                int
	SentPacketsBufferSize        int
	ReceivedPacketsBufferSize    int
	FragmentReassemblyBufferSize int
	RttSmoothingFactor           float64
	PacketLossSmoothingFactor    float64
	BandwidthSmoothingFactor     float64
	PacketHeaderSize             int

	// TransmitPacketFunction is called by SendPacket to do the actual transmitting of packets
	TransmitPacketFunction func(interface{}, int, uint16, []byte)
	// ProcessPacketFunction is called by ReceivePacket once a fully assembled packet is received
	ProcessPacketFunction func(interface{}, int, uint16, []byte) bool
	// Allocate can be used to implement custom memory allocation
	Allocate func(int) []byte
	// Free can be used to implement custom memory allocation
	Free func([]byte)
}

// NewDefaultConfig creates a typical endpoint configuration
func NewDefaultConfig() *Config {
	return &Config{
		Name:                         "endpoint",
		MaxPacketSize:                16 * 1024,
		FragmentAbove:                1024,
		MaxFragments:                 16,
		FragmentSize:                 1024,
		AckBufferSize:                256,
		SentPacketsBufferSize:        256,
		ReceivedPacketsBufferSize:    256,
		FragmentReassemblyBufferSize: 64,
		RttSmoothingFactor:           .0025,
		PacketLossSmoothingFactor:    .1,
		BandwidthSmoothingFactor:     .1,
		PacketHeaderSize:             28, // // note: UDP over IPv4 = 20 + 8 bytes, UDP over IPv6 = 40 + 8 bytes
	}
}

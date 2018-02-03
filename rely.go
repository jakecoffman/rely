package rely

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

func LessThan(s1, s2 uint16) bool {
	return GreaterThan(s2, s1)
}

func GreaterThan(s1, s2 uint16) bool {
	return ( ( s1 > s2 ) && ( s1 - s2 <= 32768 ) ) || ( ( s1 < s2 ) && ( s2 - s1  > 32768 ) )
}
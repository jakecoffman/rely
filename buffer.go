package rely

import (
	"io"
)

// buffer is a helper struct for serializing and deserializing as the caller
// does not need to externally manage where in the buffer they are currently reading or writing to.
type buffer struct {
	buf []byte
	pos int
}

func newBuffer(size int) *buffer {
	b := &buffer{}
	b.buf = make([]byte, size)
	return b
}

func newBufferFromRef(buf []byte) *buffer {
	b := &buffer{}
	b.buf = buf
	b.pos = 0
	return b
}

func (b *buffer) bytes() []byte {
	return b.buf[:b.pos]
}

func (b *buffer) reset() *buffer {
	b.pos = 0
	return b
}

func (b *buffer) getBytes(length int) ([]byte, error) {
	bufferLength := len(b.buf)
	bufferWindow := b.pos + length
	if bufferLength < length {
		return nil, io.EOF
	}
	if bufferWindow > bufferLength {
		return nil, io.EOF
	}
	value := b.buf[b.pos:bufferWindow]
	b.pos += length
	return value, nil
}

func (b *buffer) getUint8() (uint8, error) {
	buf, err := b.getBytes(sizeUint8)
	if err != nil {
		return 0, nil
	}
	return uint8(buf[0]), nil
}

func (b *buffer) getUint16() (uint16, error) {
	var n uint16
	buf, err := b.getBytes(sizeUint16)
	if err != nil {
		return 0, nil
	}
	n |= uint16(buf[0])
	n |= uint16(buf[1]) << 8
	return n, nil
}

func (b *buffer) writeByte(n byte) {
	b.buf[b.pos] = n
	b.pos++
}

func (b *buffer) writeBytes(src []byte) {
	for i := 0; i < len(src); i += 1 {
		b.writeByte(src[i])
	}
}

func (b *buffer) writeUint8(n uint8) {
	b.buf[b.pos] = byte(n)
	b.pos++
}

func (b *buffer) writeUint16(n uint16) {
	b.buf[b.pos] = byte(n)
	b.pos++
	b.buf[b.pos] = byte(n >> 8)
	b.pos++
}

const (
	sizeUint8  = 1
	sizeUint16 = 2
)

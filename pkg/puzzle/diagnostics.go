package puzzle

import (
	"bytes"
	"encoding/binary"
	"io"
)

type Diagnostics struct {
	ErrorCode     uint8
	ElapsedMillis uint32
}

func (d *Diagnostics) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, d.ErrorCode)
	binary.Write(&buf, binary.LittleEndian, d.ElapsedMillis)

	return buf.Bytes(), nil
}

func (d *Diagnostics) UnmarshalBinary(data []byte) error {
	if len(data) < (1 + 4) {
		return io.ErrShortBuffer
	}

	var offset int

	d.ErrorCode = data[0]
	offset += 1

	d.ElapsedMillis = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	return nil
}

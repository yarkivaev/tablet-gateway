package gateway

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFrameBounds(t *testing.T) {
	var b [12]byte
	binary.BigEndian.PutUint32(b[8:], 9*1024*1024)
	if _, err := ReadFrame(bytes.NewReader(b[:])); err == nil {
		t.Fatal("unbounded allocation")
	}
}
func TestFrameMetadata(t *testing.T) {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint64(1)<<63)
	binary.Write(&b, binary.BigEndian, uint32(3))
	b.Write([]byte{1, 2, 3})
	f, err := ReadFrame(&b)
	if err != nil || !f.Config || len(f.Data) != 3 {
		t.Fatal(f, err)
	}
}

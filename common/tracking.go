package common

import (
	"encoding/binary"
	"github.com/segmentio/ksuid"
	"strconv"
)

/*
func NewTrackingID() ([]byte, error) {
	id := make([]byte, 8, 8)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return id, nil
}
*/

func Hex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	ui := binary.LittleEndian.Uint64(b)
	return strconv.FormatUint(ui, 16)
}

func KSuidTo64bit(k string) [8]byte {
	var b [8]byte
	ks, err := ksuid.Parse(k)
	if err != nil {
		return b
	}

	copy(b[:], ks.Payload()[8:])
	return b
}

func KSuidTo128bit(k string) [16]byte {
	var b [16]byte
	ks, err := ksuid.Parse(k)
	if err != nil {
		return b
	}
	copy(b[:], ks.Payload()[:])
	return b
}

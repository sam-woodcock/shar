package common

import (
	"github.com/segmentio/ksuid"
)

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

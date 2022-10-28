package main

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"
)

func decodeProtoMsg(kvbyte map[string][]byte, msg proto.Message) (map[string]string, error) {

	var (
		e      error
		key    string
		b      []byte
		kvjson = make(map[string]string)
	)

	for key, b = range kvbyte {

		if msg != nil {
			if e = proto.Unmarshal(b, msg); e != nil {
				fmt.Println(e)
			}

			if b, e = json.Marshal(msg); e != nil {
				fmt.Println(e)
			}
		}

		kvjson[key] = string(b)
	}

	return kvjson, nil
}

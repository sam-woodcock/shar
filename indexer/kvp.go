package main

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

func connJS(server string) (nats.JetStreamContext, error) {

	var (
		e  error
		nc *nats.Conn
		js nats.JetStreamContext
	)

	if nc, e = nats.Connect(server); e != nil {
		return nil, e
	}

	if js, e = nc.JetStream(); e != nil {
		return nil, e
	}

	return js, nil
}

func getBucketKVP(bucket string, js nats.JetStreamContext) (map[string][]byte, error) {

	var (
		e       error
		kvstore nats.KeyValue
		kvbyte  = make(map[string][]byte)
		keys    []string
		key     string
		entry   nats.KeyValueEntry
	)

	if kvstore, e = js.KeyValue(bucket); e != nil {
		return nil, e
	}

	if keys, e = kvstore.Keys(); e != nil {
		return nil, e
	}

	for _, key = range keys {
		if entry, e = kvstore.Get(key); e != nil {
			fmt.Println(e)
		}
		kvbyte[key] = entry.Value()
	}

	return kvbyte, nil
}

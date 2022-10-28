package main

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/model"
)

func main() {

	var (
		e      error
		js     nats.JetStreamContext
		kvbyte map[string][]byte
		kvjson map[string]string
		store  = "WORKFLOW_VERSION"
	)

	if js, e = connJS("nats://localhost:4222"); e != nil {
		fmt.Println(e)
	}

	if kvbyte, e = getBucketKVP(store, js); e != nil {
		fmt.Println(e)
	}

	if kvjson, e = decodeProtoMsg(kvbyte, &model.WorkflowVersions{}); e != nil {
		fmt.Println(e)
	}

	if e = newIndex(store); e != nil {
		fmt.Println(e)
	}

	if e = addKvJson(store, kvjson); e != nil {
		fmt.Println(e)
	}

	if e = valueMatch(store, "2GiMAtRckFb5RPzWTh4gcjxfrQt"); e != nil {
		fmt.Println(e)
	}

}

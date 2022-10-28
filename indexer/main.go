package main

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/model"
	"google.golang.org/protobuf/proto"
)

func main() {

	var (
		e error
	)

	// Name is just an name/id now proto message within
	if e = syncWithModel("WORKFLOW_NAME", nil); e != nil {
		fmt.Println(e)
	}

	// Name is just an name/id now proto message within
	if e = syncWithModel("WORKFLOW_CLIENTTASK", nil); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_VERSION", &model.WorkflowVersions{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_MSGSUBS", &model.WorkflowInstanceSubscribers{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_INSTANCE", &model.WorkflowInstance{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_JOB", &model.WorkflowState{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_VARSTATE", &model.WorkflowState{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_DEF", &model.Workflow{}); e != nil {
		fmt.Println(e)
	}

	if e = syncWithModel("WORKFLOW_TRACKING", &model.WorkflowState{}); e != nil {
		fmt.Println(e)
	}

	// This is a find, it will match any index value, or json key/value but not index id (same as bucket index reference)
	if e = valueMatch("WORKFLOW_VERSION", "sha256"); e != nil {
		fmt.Println(e)
	}

}

func syncWithModel(store string, msg proto.Message) error {

	var (
		e      error
		js     nats.JetStreamContext
		kvbyte map[string][]byte
		kvjson map[string]string
	)

	if js, e = connJS("nats://localhost:4222"); e != nil {
		return e
	}

	if kvbyte, e = getBucketKVP(store, js); e != nil {
		return e
	}

	if kvjson, e = decodeProtoMsg(kvbyte, msg); e != nil {
		return e
	}

	if e = newIndex(store); e != nil {
		return e
	}

	if e = addKvJson(store, kvjson); e != nil {
		return e
	}

	return nil
}

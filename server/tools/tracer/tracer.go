package tracer

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/model"
	"google.golang.org/protobuf/proto"
	"strings"
)

func Trace(natsUrl string) *nats.Subscription {
	nc, _ := nats.Connect(natsUrl)
	sub, err := nc.Subscribe("WORKFLOW.>", func(msg *nats.Msg) {
		if strings.HasPrefix(msg.Subject, "WORKFLOW.State.") {
			d := &model.WorkflowState{}
			proto.Unmarshal(msg.Data, d)
			fmt.Println(msg.Subject, d.WorkflowInstanceId, d.Id, d.ParentId, d.ElementType, d.ElementId)
		} else {
			fmt.Println(msg.Subject)
		}
	})
	if err != nil {
		panic(err)
	}
	return sub
}

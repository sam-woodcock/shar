package tracer

import (
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"google.golang.org/protobuf/proto"
	"strings"
)

func Trace(natsUrl string) *nats.Subscription {
	nc, _ := nats.Connect(natsUrl)
	sub, err := nc.Subscribe("WORKFLOW.>", func(msg *nats.Msg) {
		if strings.HasPrefix(msg.Subject, "WORKFLOW.default.State.") {
			d := &model.WorkflowState{}
			err := proto.Unmarshal(msg.Data, d)
			if err != nil {
				panic(err)
			}
			fmt.Println(msg.Subject, d.State, Last4(d.WorkflowInstanceId), "T:"+Last4(common.TrackingID(d.Id).ID()), "P:"+Last4(common.TrackingID(d.Id).ParentID()), d.ElementType, d.ElementId)
		} else {
			fmt.Println(msg.Subject)
		}
	})
	if err != nil {
		panic(err)
	}
	return sub
}

func Last4(s string) string {
	if len(s) < 4 {
		return ""
	}
	return s[len(s)-4:]
}

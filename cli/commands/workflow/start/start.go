package start

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/subj"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/valueparsing"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var Cmd = &cobra.Command{
	Use:   "start",
	Short: "Starts a new workflow instance",
	Long:  `shar workflow start "WorkflowName"`,
	RunE:  run,
	Args:  cobra.ExactValidArgs(1),
}

func run(_ *cobra.Command, args []string) error {
	vars := &model.Vars{}
	var err error
	if len(flag.Value.Vars) > 0 {
		vars, err = valueparsing.Parse(flag.Value.Vars)
		if err != nil {
			return err
		}
	}

	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	wfiID, err := shar.LaunchWorkflow(ctx, args[0], *vars)
	if err != nil {
		return fmt.Errorf("workflow launch failed: %w", err)
	}
	fmt.Println("workflow instance started. instance-id:", wfiID)

	if flag.Value.DebugTrace {
		// Create logger
		log, _ := zap.NewDevelopment()

		// Connect to a server
		nc, _ := nats.Connect(nats.DefaultURL)

		// Get Jetstream
		js, err := nc.JetStream()
		if err != nil {
			panic(err)
		}

		if err := common.EnsureBuckets(js, nats.FileStorage, []string{"WORKFLOW_DEBUG"}); err != nil {
			panic(err)
		}

		if err := EnsureConsumer(js, "WORKFLOW", &nats.ConsumerConfig{
			Durable:       "Tracing",
			Description:   "Sequential Trace Consumer",
			DeliverPolicy: nats.DeliverAllPolicy,
			FilterSubject: subj.NS(messages.WorkflowStateAll, "default"),
			AckPolicy:     nats.AckExplicitPolicy,
		}); err != nil {
			panic(err)
		}

		ctx = context.Background()
		closer := make(chan struct{})
		workflowMessages := make(chan *nats.Msg)

		err = common.Process(ctx, js, log, "trace", closer, subj.NS(messages.WorkflowStateAll, "*"), "Tracing", 1, func(ctx context.Context, msg *nats.Msg) (bool, error) {
			workflowMessages <- msg
			return true, nil
		})
		if err != nil {
			return err
		}

		c := output.Console{}
		for msg := range workflowMessages {
			var state = model.WorkflowState{}
			err := proto.Unmarshal(msg.Data, &state)
			if err != nil {
				log.Error("unable to unmarshal message", zap.Error(err))
				return err
			}
			if state.WorkflowInstanceId == wfiID {
				err := c.OutputWorkflowInstanceStatus([]*model.WorkflowState{&state})
				if err != nil {
					return err
				}
			}
			// Check end states once they are implemented
			// if state.State == "" {
			// 	close(closer)
			// 	close(workflowMessages)
			// }
		}
	}
	return nil
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&flag.Value.DebugTrace, flag.DebugTrace, flag.DebugTraceShort, false, "enable debug trace for selected workflow")
	Cmd.PersistentFlags().StringSliceVarP(&flag.Value.Vars, flag.Vars, flag.VarsShort, []string{}, "pass variables to given workflow, eg --vars \"orderId:int(78),serviceId:string(hello)\"")
}

func EnsureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); err == nats.ErrConsumerNotFound {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			panic(err)
		}
	} else if err != nil {
		return err
	}
	return nil
}

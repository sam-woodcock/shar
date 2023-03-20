package start

import (
	"context"
	"errors"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"golang.org/x/exp/slog"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/valueparsing"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"google.golang.org/protobuf/proto"
)

// Cmd is the cobra command object
var Cmd = &cobra.Command{
	Use:   "start",
	Short: "Starts a new workflow instance",
	Long:  `shar workflow start "WorkflowName"`,
	RunE:  run,
	Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
}

func run(cmd *cobra.Command, args []string) error {
	if err := cmd.ValidateArgs(args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	vars := &model.Vars{}
	var err error
	if len(flag.Value.Vars) > 0 {
		vars, err = valueparsing.Parse(flag.Value.Vars)
		if err != nil {
			return fmt.Errorf("parse flags: %w", err)
		}
	}

	ctx := context.Background()
	shar := client.New()
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("dialling server: %w", err)
	}
	wfiID, wfID, err := shar.LaunchWorkflow(ctx, args[0], *vars)
	if err != nil {
		return fmt.Errorf("workflow launch failed: %w", err)
	}

	if flag.Value.DebugTrace {
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

		err = common.Process(ctx, js, "trace", closer, subj.NS(messages.WorkflowStateAll, "*"), "Tracing", 1, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
			workflowMessages <- msg
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("starting debug trace processing: %w", err)
		}

		for msg := range workflowMessages {
			var state = model.WorkflowState{}
			err := proto.Unmarshal(msg.Data, &state)
			if err != nil {
				log := logx.FromContext(ctx)
				log.Error("unmarshal message", err)
				return fmt.Errorf("unmarshal status trace message: %w", err)
			}
			//if state.WorkflowInstanceId == wfiID {
			//TODO: Re- implement
			//	output.Current.OutputWorkflowInstanceStatus(wfiID, []*model.WorkflowState{&state})
			//}
			// Check end states once they are implemented
			// if state.State == "" {
			// 	close(closer)
			// 	close(workflowMessages)
			// }
		}
	}
	output.Current.OutputStartWorkflowResult(wfiID, wfID)
	return nil
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&flag.Value.DebugTrace, flag.DebugTrace, flag.DebugTraceShort, false, "enable debug trace for selected workflow")
	Cmd.PersistentFlags().StringSliceVarP(&flag.Value.Vars, flag.Vars, flag.VarsShort, []string{}, "pass variables to given workflow, eg --vars \"orderId:int(78),serviceId:string(hello)\"")
}

// EnsureConsumer sets up the consumer in NATS if one doesn't exist already
func EnsureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); errors.Is(err, nats.ErrConsumerNotFound) {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			panic(err)
		}
	} else if err != nil {
		return fmt.Errorf("ensuring consumer: %w", err)
	}
	return nil
}

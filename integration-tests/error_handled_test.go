package intTests

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestHandledError(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log)
	if err := cl.Dial(natsURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/errors.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestHandleError", b); err != nil {
		panic(err)
	}

	d := errorHandledHandlerDef{}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "TestHandleError", model.Vars{})
	if err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()

	// wait for the workflow to complete
	for i := range complete {
		if i.WorkflowInstanceId == wfiID {
			break
		}
	}
}

type errorHandledHandlerDef struct {
	fixed bool
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) mayFail(_ context.Context, _ model.Vars) (model.Vars, error) {
	fmt.Println("Throw handled error")
	return model.Vars{"success": false}, workflow.Error{Code: "101", WrappedError: errors.New("things went badly")}
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) fixSituation(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Fixing")
	fmt.Println("carried", vars["carried"])
	d.fixed = true
	return model.Vars{}, nil
}

package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"sync"
	"testing"
	"time"
)

func TestUserTasks(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	if err := cl.Dial(tst.NatsURL); err != nil {
		panic(err)
	}

	//sub := tracer.Trace(NatsURL)
	//defer sub.Drain()

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/usertask.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestUserTasks", b); err != nil {
		panic(err)
	}

	d := &testUserTaskHandlerDef{finished: make(chan struct{})}
	d.finalVars = make(model.Vars)
	// Register a service task
	err = cl.RegisterServiceTask(ctx, "Prepare", d.prepare)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "Complete", d.complete)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "TestUserTasks", model.Vars{"OrderId": 68})
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

	go func() {
		for {
			tsk, err := cl.ListUserTaskIDs(ctx, "andrei")
			require.NoError(t, err)
			if err == nil && tsk.Id != nil {
				td, _, gerr := cl.GetUserTask(ctx, "andrei", tsk.Id[0])
				assert.NoError(t, gerr)
				fmt.Println("Name:", td.Name)
				fmt.Println("Description:", td.Description)
				cerr := cl.CompleteUserTask(ctx, "andrei", tsk.Id[0], model.Vars{"Forename": "Brangelina", "Surname": "Miggins"})
				assert.NoError(t, cerr)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()

	<-d.finished

	et, err := cl.ListUserTaskIDs(ctx, "andrei")
	assert.NoError(t, err)
	assert.Equal(t, 0, len(et.Id))
	d.lock.Lock()
	defer d.lock.Unlock()
	assert.Equal(t, "Brangelina", d.finalVars["Forename"].(string))
	assert.Equal(t, "Miggins", d.finalVars["Surname"].(string))
	assert.Equal(t, 69, d.finalVars["OrderId"].(int))
	assert.Equal(t, 32767, d.finalVars["carried"].(int))
	tst.AssertCleanKV()
}

type testUserTaskHandlerDef struct {
	finalVars model.Vars
	lock      sync.Mutex
	finished  chan struct{}
}

// A "Hello World" service task
func (d *testUserTaskHandlerDef) prepare(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Preparing")
	oid := vars["OrderId"].(int)
	return model.Vars{"OrderId": oid + 1}, nil
}

// A "Hello World" service task
func (d *testUserTaskHandlerDef) complete(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Completed")
	fmt.Println("OrderId", vars["OrderId"])
	fmt.Println("Forename", vars["Forename"])
	fmt.Println("Surname", vars["Surname"])
	fmt.Println("carried", vars["carried"])
	d.lock.Lock()
	defer d.lock.Unlock()
	d.finalVars = vars
	return model.Vars{}, nil
}

func (d *testUserTaskHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}

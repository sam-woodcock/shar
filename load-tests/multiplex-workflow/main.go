package main

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"sync"
	"time"
)

var finished = make(chan struct{})

func main() {
	// Create a starting context
	ctx := context.Background()
	// Dial shar
	cl1 := client.New()
	cl2 := client.New()
	if err := cl1.Dial(ctx, nats.DefaultURL); err != nil {
		panic(err)
	}
	if err := cl2.Dial(ctx, nats.DefaultURL); err != nil {
		panic(err)
	}

	// Load first workflow
	b, err := os.ReadFile("testdata/load-workflow1.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl1.LoadBPMNWorkflowFromBytes(ctx, "LoadDemo1", b); err != nil {
		panic(err)
	}

	// Load second workflow
	b, err = os.ReadFile("testdata/load-workflow2.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl2.LoadBPMNWorkflowFromBytes(ctx, "LoadDemo2", b); err != nil {
		panic(err)
	}

	// Load sub workflow
	b, err = os.ReadFile("testdata/load-sub-workflow.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl2.LoadBPMNWorkflowFromBytes(ctx, "LoadTestSubworkflow", b); err != nil {
		panic(err)
	}

	// Register a service task
	err = cl1.RegisterServiceTask(ctx, "ReadyTask", readyTask)
	if err != nil {
		panic(err)
	}
	err = cl2.RegisterServiceTask(ctx, "SteadyTask", steadyTask)
	if err != nil {
		panic(err)
	}
	err = cl1.RegisterServiceTask(ctx, "GoTask", goTask)
	if err != nil {
		panic(err)
	}
	err = cl2.RegisterServiceTask(ctx, "WinTask", winTask)
	if err != nil {
		panic(err)
	}
	err = cl1.RegisterServiceTask(ctx, "LoseTask", loseTask)
	if err != nil {
		panic(err)
	}
	err = cl2.RegisterServiceTask(ctx, "Stage1Task", stage1Task)
	if err != nil {
		panic(err)
	}
	err = cl1.RegisterServiceTask(ctx, "Stage2Task", stage2Task)
	if err != nil {
		panic(err)
	}
	err = cl2.RegisterServiceTask(ctx, "Sub1Task", sub1Task)
	if err != nil {
		panic(err)
	}

	// A hook to watch for completion
	err = cl1.RegisterProcessComplete("Process_03llwnm", processEnd)
	if err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl1.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()
	// Listen for service tasks
	go func() {
		err := cl2.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()

	sw := time.Now()
	wg := sync.WaitGroup{}
	n := 5000
	for i := 0; i < n; i++ {
		// Launch the workflows
		_, _, err := cl2.LaunchWorkflow(ctx, "LoadDemo1", model.Vars{})
		if err != nil {
			panic(err)
		}
		_, _, err = cl2.LaunchWorkflow(ctx, "LoadDemo2", model.Vars{})
		if err != nil {
			panic(err)
		}
		wg.Add(2)
	}
	// wait for the workflow to complete
	for i := 0; i < n*2; i++ {
		<-finished
		wg.Done()
	}
	wg.Wait()
	fmt.Println(-time.Until(sw))
}

func stage1Task(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("stage1")
	return model.Vars{}, nil
}

func stage2Task(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("stage2")
	return model.Vars{}, nil
}

func readyTask(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("ready")
	time.Sleep(1 * time.Second)
	return model.Vars{}, nil
}

func steadyTask(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("steady")
	return model.Vars{}, nil
}

func goTask(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("go")
	return model.Vars{"IsWinner": true}, nil
}

func winTask(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("win")
	return model.Vars{}, nil
}

func loseTask(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//fmt.Println("lose")
	return model.Vars{}, nil
}

func sub1Task(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	time.Sleep(1 * time.Second)
	//fmt.Println("sub workflow task 1")
	return model.Vars{}, nil
}

func processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	finished <- struct{}{}
}

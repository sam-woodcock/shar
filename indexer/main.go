package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/indexer/inmemorytable"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/telemetry/config"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func main() {

	var (
		e          error
		cfg        *config.Settings
		nc         *nats.Conn
		js         nats.JetStreamContext
		tablesLock sync.Mutex
		tables     = make(map[string]*inmemorytable.Table)
	)

	// Get the configuration
	cfg, e = config.GetEnvironment()
	if e != nil {
		panic(e)
	}

	// Connect to nats
	nc, e = nats.Connect(cfg.NatsURL)
	if e != nil {
		panic(e)
	}

	// Connect to nats Jetstream
	if js, e = nc.JetStream(); e != nil {
		panic(e)
	}

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_NAME", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_CLIENTTASK", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_VERSION", &model.WorkflowVersions{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_MSGSUBS", &model.WorkflowInstanceSubscribers{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_INSTANCE", &model.WorkflowInstance{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_JOB", &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_VARSTATE", &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_DEF", &model.Workflow{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_TRACKING", &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_MSGSUB", &model.WorkflowInstanceSubscribers{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_MSGNAME", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_MSGID", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_USERTASK", &model.UserTasks{}); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_OWNERID", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// Load and maintain state for the in memory table
	go func() {
		// Listen and store updates for the table
		if e = trackBucket(js, tables, &tablesLock, "WORKFLOW_OWNERNAME", nil); e != nil {
			fmt.Println(e)
		}
	}()

	// time.Sleep(time.Second)
	// // Testing entry
	// *tables["WORKFLOW_USERTASK"].Add("1") = model.WorkflowState{
	// 	WorkflowId:         "2IUjNaqAB54CPtUitssbsGXymDi",
	// 	WorkflowInstanceId: "wfiid",
	// 	ElementId:          "elementid",
	// 	ElementType:        "elementType",
	// 	Id:                 []string{"id1", "id2"},
	// 	Owners:             []string{"user1"},
	// }
	// *tables["WORKFLOW_USERTASK"].Add("2") = model.WorkflowState{
	// 	WorkflowId:         "2IUjNaqAB54CPtUitssbsGXymDi",
	// 	WorkflowInstanceId: "wfiid2",
	// 	ElementId:          "elementid2",
	// 	ElementType:        "elementType2",
	// 	Id:                 []string{"id3", "id4"},
	// 	Owners:             []string{"user1"},
	// }

	// Listen and respond to nats questions for this table
	if _, e := nc.QueueSubscribe(messages.APIListUserTaskIDs, messages.APIListUserTaskIDs, func(msg *nats.Msg) {
		var (
			e            error
			b            []byte
			req          = model.ListUserTasksRequest{}
			userTaskList = model.UserTasks{}
		)

		if e = proto.Unmarshal(msg.Data, &req); e != nil {
			fmt.Println(e)
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|" + e.Error()},
			})
			msg.Respond(reply)
			return
		}

		// Check table exists
		if _, ok := tables["WORKFLOW_USERTASK"]; !ok {
			fmt.Println("query to WORKFLOW_USERTASK table that does not exist")
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|unable to answer query, please try later"},
			})
			msg.Respond(reply)
			return
		}

		// Loop over all instances workflow state
		for id, item := range tables["WORKFLOW_USERTASK"].List() {

			// Loop over all the Owners looking for a match
			for _, owners := range (*item).(model.WorkflowState).Owners {
				if owners == req.Owner {
					// If found add teh ID to the response Id list
					userTaskList.Id = append(userTaskList.Id, id)
				}
			}
		}

		b, _ = proto.Marshal(&userTaskList)
		msg.Respond(b)

	}); e != nil {
		fmt.Println(e)
	}

	// Respond to APIGetUserTask
	if _, e := nc.QueueSubscribe(messages.APIGetUserTask, messages.APIGetUserTask, func(msg *nats.Msg) {
		var (
			e             error
			b             []byte
			ok            bool
			req           = model.GetUserTaskRequest{}
			resp          = model.GetUserTaskResponse{}
			usertask, def *any
		)

		// Unmarshal the message
		if e = proto.Unmarshal(msg.Data, &req); e != nil {
			fmt.Println(e)
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|" + e.Error()},
			})
			msg.Respond(reply)
			return
		}

		// Check table exists
		if _, ok := tables["WORKFLOW_USERTASK"]; !ok {
			fmt.Println("query to WORKFLOW_USERTASK table that does not exist")
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|unable to answer query, please try later"},
			})
			msg.Respond(reply)
			return
		}

		// Loop over all instances workflow state
		// Find the usertask based on the ID
		if usertask, ok = tables["WORKFLOW_USERTASK"].Get(req.TrackingId); !ok {
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|key not found in usertask bucket"},
			})
			msg.Respond(reply)
			return
		}

		// Check table exists
		if _, ok := tables["WORKFLOW_DEF"]; !ok {
			fmt.Println("query to WORKFLOW_Def table that does not exist")
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|unable to answer query, please try later"},
			})
			msg.Respond(reply)
			return
		}

		// Extract the defination of this job
		if def, ok = tables["WORKFLOW_DEF"].Get((*usertask).(model.WorkflowState).WorkflowId); !ok {
			reply, _ := proto.Marshal(&model.WorkflowState{
				Error: &model.Error{Code: " ERR_3|key not found in definition bucket"},
			})
			msg.Respond(reply)
			return
		}

		// Itterate over defination elements to find an element ID match
		for _, process := range (*def).(*model.Workflow).Process {
			for _, element := range process.Elements {
				if element.Id == (*usertask).(model.WorkflowState).ElementId {
					resp.Name = element.Name
					resp.Description = element.Documentation
					break
				}
			}
		}

		// Get the last tracking ID
		// Unsure if this is always the same as the provided trackingID
		if len((*usertask).(model.WorkflowState).Id) > 0 {
			resp.TrackingId = (*usertask).(model.WorkflowState).Id[len((*usertask).(model.WorkflowState).Id)-1]
		}

		resp.Vars = (*usertask).(model.WorkflowState).Vars
		resp.Owner = req.Owner

		b, _ = proto.Marshal(&resp)
		msg.Respond(b)

	}); e != nil {
		fmt.Println(e)
	}

	<-make(chan struct{})
}

func trackBucket(js nats.JetStreamContext, tables map[string]*inmemorytable.Table, tablesLock *sync.Mutex, store string, msgstruct protoreflect.ProtoMessage) error {

	var (
		e       error
		watcher <-chan nats.KeyValueEntry
		w       nats.KeyValueEntry
		t       *inmemorytable.Table
		ok      bool
	)

	// Create table if it does not exist already (Which it should not)
	tablesLock.Lock()
	if _, ok = tables[store]; ok {
		tablesLock.Unlock()
		return fmt.Errorf("table and Listener I expect already exists")
	}
	t = inmemorytable.New()
	tables[store] = t
	tablesLock.Unlock()

	if watcher, e = getBucketWatcher(js, store); e != nil {
		return e
	}

	// Ranges over bucket state changes
	for w = range watcher {

		if w != nil {
			switch w.Operation() {
			case nats.KeyValueDelete:
				t.Delete(w.Key())
			case nats.KeyValuePurge:
				t.Purge()
			case nats.KeyValuePut:
				// msgstruct has an embedded pointer, we need to clone to get a unique pointer to an in memory space, where we can save data.
				msgstruct := proto.Clone(msgstruct)

				// Return a pointer to our in-memory table
				// Where we can save the data
				x := t.Add(w.Key())

				// Process the case, where the expected data is declared to be a specific struct
				if msgstruct != nil {
					// Extract the message into the given type
					if e = proto.Unmarshal(w.Value(), msgstruct); e != nil {
						fmt.Println(e)
					}
					*x = msgstruct
				} else {
					// Process the case where the message is not expected to be a specific struct, assume just string
					*x = string(w.Value())
				}
			}
		} else {
			// Handle the nil case where we are up-to-date
			continue
		}
	}

	return nil

}

func getBucketWatcher(js nats.JetStreamContext, bucket string) (<-chan nats.KeyValueEntry, error) {

	var (
		e       error
		kvstore nats.KeyValue
		watcher nats.KeyWatcher
	)

	if kvstore, e = js.KeyValue(bucket); e != nil {
		return nil, e
	}

	if watcher, e = kvstore.Watch("*"); e != nil {
		return nil, e
	} else {
		return watcher.Updates(), nil
	}
}

// Decode decodes a go binary object containing workflow variables.
func Decode(vars []byte) (model.Vars, error) {
	ret := make(map[string]any)
	if len(vars) == 0 {
		return ret, nil
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if e := d.Decode(&ret); e != nil {
		msg := "failed to decode vars"
		return nil, fmt.Errorf(msg+": %w", &errors.ErrWorkflowFatal{Err: e})
	}
	return ret, nil
}

package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/telemetry/config"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var listenAddr = os.Getenv("listenAddr")

func main() {

	var (
		e error
		//cfg *config.settings
		nc *nats.Conn
		js nats.JetStreamContext
	)

	// Get the configuration
	cfg, e := config.GetEnvironment()
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

	// This is the workflows registered by 'loadBPMN' function, with an ID
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_NAME", "/workflow-name", "/ws-name", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	// This is functions registered by the 'RegisterServiceTask', with an ID
	// There is no strict relationship between the registered task and the model, only a shared name.
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_CLIENTTASK", "/workflow-clienttask", "/ws-clienttask", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	// This contains BPMN Model name, and a list of SHA hash with a version
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_VERSION", "/workflow-version", "/ws-version", memoryTable, &model.WorkflowVersions{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Inititate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_MSGSUBS", "/workflow-msgsubs", "/ws-msgsubs", memoryTable, &model.WorkflowInstanceSubscribers{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currenly running instances
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_INSTANCE", "/workflow-instance", "/ws-instance", memoryTable, &model.WorkflowInstance{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is the main model, telling us about all tasks in progress.
	// TODO: This might just be the last update of the workflow, it might not be all tasks.
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_JOB", "/workflow-job", "/ws-job", memoryTable, &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_VARSTATE", "/workflow-varstate", "/ws-varstate", memoryTable, &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This 'key' is the workflowID, it matches the WORKFLOW_VERSION 'key'
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_DEF", "/workflow-def", "/ws-def", memoryTable, &model.Workflow{}); e != nil {
			fmt.Println(e)
		}
	}()

	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_TRACKING", "/workflow-tracking", "/ws-tracking", memoryTable, &model.WorkflowState{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_MSGSUB", "/workflow-msgsub", "/ws-msgsub", memoryTable, &model.WorkflowInstanceSubscribers{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_MSGNAME", "/workflow-msgname", "/ws-msgname", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Initiate new table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_MSGID", "/workflow-msgid", "/ws-msgid", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and respond to nats questions for this table
		if _, e := nc.QueueSubscribe("WORKFLOW.Api.ListUserTaskIDs", "WORKFLOW.Api.ListUserTaskIDs", func(msg *nats.Msg) {
			var (
				e   error
				b   bool
				m   *any
				req = model.ListUserTasksRequest{}
			)

			fmt.Println("New ListUserTaskIDs request")

			if e = proto.Unmarshal(msg.Data, &req); e != nil {
				fmt.Println(e)
				res, _ := proto.Marshal(&model.WorkflowState{})
				msg.Respond(res)
				return
			}

			if m, b = memoryTable.Get(req.Owner); !b {
				res, _ := proto.Marshal(&model.WorkflowState{})
				msg.Respond(res)
				return
			}

			res, _ := proto.Marshal((*m).(*model.WorkflowState))
			msg.Respond(res)

		}); e != nil {
			fmt.Println(e)
		}

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_USERTASK", "/workflow-usertask", "/ws-usertask", memoryTable, &model.UserTasks{}); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Initiate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_OWNERID", "/workflow-ownerid", "/ws-ownerid", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	// This is currently running instances, listed against the WorkflowID
	go func() {
		// Inititate the table
		var memoryTable = New()

		// Listen and store updates for the table
		if e = trackBucket(js, "WORKFLOW_OWNERNAME", "/workflow-ownername", "/ws-ownername", memoryTable, nil); e != nil {
			fmt.Println(e)
		}
	}()

	go func() {
		if listenAddr != "" {
			fmt.Println("Started HTTP handler on", listenAddr)
			fmt.Println(http.ListenAndServe(listenAddr, nil))
		}
	}()

	<-make(chan struct{})
}

func trackBucket(js nats.JetStreamContext, store, httpEndpoint, wsEndpoint string, t *inMemoryTable, msg protoreflect.ProtoMessage) error {

	var (
		e       error
		watcher <-chan nats.KeyValueEntry
		w       nats.KeyValueEntry
		wschans = make(map[string]chan []byte)
		b       []byte
	)

	if watcher, e = getBucketWatcher(js, store); e != nil {
		return e
	}

	// Start a http json endpoint
	http.HandleFunc(httpEndpoint, t.HTTPServe)

	// Start a websocket sender
	http.Handle(wsEndpoint, websocket.Handler(func(ws *websocket.Conn) {

		// I can't find the socket information, so well use a random string
		id := RandomString(10)

		// Make a message queue
		wschan := make(chan []byte, 100)

		// Register our message queue
		wschans[id] = wschan

		// Whatever happens remove our message queue
		defer func() {
			delete(wschans, id)
		}()

		var (
			b []byte
			e error
		)

		fmt.Println("Websocket Connection to", ws.LocalAddr(), "from", ws.RemoteAddr())

		// Loop sending updates as they arrive on the channel
		for {
			b = <-wschan
			if _, e = ws.Write(b); e != nil {
				fmt.Println(e)
			}

		}
	}))

	for w = range watcher {

		// Handle the nil case where we are up-to-date
		if w == nil {
			fmt.Println(store, ": Updated")
			continue
		}

		msg := proto.Clone(msg)

		if w != nil {
			x := t.Add(w.Key())

			if msg != nil {
				if e = proto.Unmarshal(w.Value(), msg); e != nil {
					fmt.Println(e)
				}
				(*x) = msg

				if b, e = json.Marshal(map[string]protoreflect.ProtoMessage{
					w.Key(): msg},
				); e != nil {
					fmt.Println(e)
					continue
				} else {
					for _, wschan := range wschans {
						select {
						case wschan <- b:
						default:
						}
					}
				}

			} else {
				(*x) = string(w.Value())
				if b, e = json.Marshal(map[string]string{
					w.Key(): string(w.Value())},
				); e != nil {
					fmt.Println(e)
					continue
				} else {
					for _, wschan := range wschans {
						select {
						case wschan <- b:
						default:
						}
					}
				}
			}
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

func (t *inMemoryTable) HTTPServe(w http.ResponseWriter, r *http.Request) {
	t.RLock()
	defer t.RUnlock()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	b, e := t.JSON()

	if e != nil {
		fmt.Println(e)
		w.WriteHeader(502)
		return
	}

	w.Write(b)
}

// Decode decodes a go binary object containing workflow variables.
func Decode(vars []byte) (model.Vars, error) {
	ret := make(map[string]any)
	if len(vars) == 0 {
		return ret, nil
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		msg := "failed to decode vars"
		//		log.Error(msg, zap.Any("vars", vars), zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", &errors.ErrWorkflowFatal{Err: err})
	}
	return ret, nil
}

func RandomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

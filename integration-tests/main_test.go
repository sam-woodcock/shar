package intTests

import (
	"context"
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/model"
	sharsvr "gitlab.com/shar-workflow/shar/server/server"
	zensvr "gitlab.com/shar-workflow/shar/zen-shar/server"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"sync"
	"testing"
	"time"
)

const natsHost = "127.0.0.1"
const natsPort = 4459

var natsURL = fmt.Sprintf("nats://%s:%v", natsHost, natsPort)

type integration struct {
	testNatsServer *server.Server
	testSharServer *sharsvr.Server
	finalVars      map[string]interface{}
	test           *testing.T
	mx             sync.Mutex
	cooldown       time.Duration
}

//goland:noinspection GoNilness
func (s *integration) setup(t *testing.T) {
	s.cooldown = 2 * time.Second
	s.test = t
	s.finalVars = make(map[string]interface{})
	logger, err := zap.Config{
		Level:            zap.NewAtomicLevelAt(zap.DebugLevel),
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}.Build()
	if err != nil {
		panic(err)
	}
	ss, ns, err := zensvr.GetServers(natsHost, natsPort, logger)
	if err != nil {
		panic(err)
	}
	s.testSharServer = ss
	s.testNatsServer = ns
	s.test.Logf("\033[1;36m%s\033[0m", "> Setup completed\n")
}

func (s *integration) AssertCleanKV() {
	time.Sleep(s.cooldown)
	js, err := s.GetJetstream()
	require.NoError(s.test, err)

	for n := range js.KeyValueStores() {
		name := n.Bucket()
		kvs, err := js.KeyValue(name)
		require.NoError(s.test, err)
		keys, err := kvs.Keys()
		if err != nil && err.Error() == "nats: no keys found" {
			continue
		}
		require.NoError(s.test, err)
		switch name {
		case "WORKFLOW_DEF",
			"WORKFLOW_NAME",
			"WORKFLOW_VERSION",
			"WORKFLOW_CLIENTTASK",
			"WORKFLOW_MSGID",
			"WORKFLOW_MSGNAME",
			"WORKFLOW_OWNERNAME",
			"WORKFLOW_OWNERID",
			"WORKFLOW_USERTASK":
			//noop
		default:
			assert.Len(s.test, keys, 0, n.Bucket())
		}
	}

	b, err := js.KeyValue("WORKFLOW_USERTASK")
	if err != nil && err.Error() != "nats: no keys found" {
		if err != nil {
			s.test.Error(err)
			return
		}
		keys, err := b.Keys()
		if err != nil {
			s.test.Error(err)
			return
		}
		for _, k := range keys {
			bts, err := b.Get(k)
			if err != nil {
				s.test.Error(err)
				return
			}
			msg := &model.UserTasks{}
			err = proto.Unmarshal(bts.Value(), msg)
			if err != nil {
				s.test.Error(err)
				return
			}
			assert.Len(s.test, msg.Id, 0)
		}
	}
}

func (s *integration) teardown() {

	n, err := s.GetJetstream()
	require.NoError(s.test, err)

	sub, err := n.PullSubscribe("WORKFLOW.>", "fin")
	require.NoError(s.test, err)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err := sub.Fetch(1, nats.Context(ctx))
		if err == context.DeadlineExceeded {
			cancel()
			break
		}
		if err != nil {
			cancel()
			s.test.Fatal(err)
		}

		cancel()
	}
	s.test.Log("TEARDOWN")
	s.testSharServer.Shutdown()
	s.testNatsServer.Shutdown()
	s.testNatsServer.WaitForShutdown()
	s.test.Log("NATS shut down")
	s.test.Logf("\033[1;36m%s\033[0m", "> Teardown completed")
	s.test.Log("\n")
}

func (s *integration) GetJetstream() (nats.JetStreamContext, error) { //nolint:ireturn
	con, err := s.GetNats()
	if err != nil {
		return nil, err
	}
	js, err := con.JetStream()
	if err != nil {
		return nil, err
	}
	return js, nil
}

func (s *integration) GetNats() (*nats.Conn, error) {
	con, err := nats.Connect(natsURL)
	if err != nil {
		return nil, err
	}
	return con, nil
}

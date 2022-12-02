package intTest

import (
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/model"
	sharsvr "gitlab.com/shar-workflow/shar/server/server"
	zensvr "gitlab.com/shar-workflow/shar/zen-shar/server"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"sync"
	"testing"
	"time"
)

const NatsHost = "127.0.0.1"
const NatsPort = 4459

var NatsURL = fmt.Sprintf("nats://%s:%v", NatsHost, NatsPort)

type Integration struct {
	testNatsServer *server.Server
	testSharServer *sharsvr.Server
	finalVars      map[string]interface{}
	test           *testing.T
	mx             sync.Mutex
	cooldown       time.Duration
}

func init() {
	logx.SetDefault(slog.ErrorLevel, false, "shar-Integration-tests")
}

//goland:noinspection GoNilness
func (s *Integration) Setup(t *testing.T) {

	s.cooldown = 2 * time.Second
	s.test = t
	s.finalVars = make(map[string]interface{})
	ss, ns, err := zensvr.GetServers(NatsHost, NatsPort, 10)
	if err != nil {
		panic(err)
	}
	s.testSharServer = ss
	s.testNatsServer = ns
	s.test.Logf("\033[1;36m%s\033[0m", "> Setup completed\n")
}

func (s *Integration) AssertCleanKV() {
	time.Sleep(s.cooldown)
	js, err := s.GetJetstream()
	require.NoError(s.test, err)

	for n := range js.KeyValueStores() {
		name := n.Bucket()
		kvs, err := js.KeyValue(name)
		require.NoError(s.test, err)
		keys, err := kvs.Keys()
		if err != nil && errors.Is(err, nats.ErrNoKeysFound) {
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
	if err != nil && errors.Is(err, nats.ErrNoKeysFound) {
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

func (s *Integration) Teardown() {

	n, err := s.GetJetstream()
	require.NoError(s.test, err)

	sub, err := n.PullSubscribe("WORKFLOW.>", "fin")
	require.NoError(s.test, err)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err := sub.Fetch(1, nats.Context(ctx))
		if errors.Is(err, context.DeadlineExceeded) {
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

func (s *Integration) GetJetstream() (nats.JetStreamContext, error) { //nolint:ireturn
	con, err := s.GetNats()
	if err != nil {
		return nil, fmt.Errorf("could not get NATS: %w", err)
	}
	js, err := con.JetStream()
	if err != nil {
		return nil, fmt.Errorf("could not obtain JetStream connection: %w", err)
	}
	return js, nil
}

func (s *Integration) GetNats() (*nats.Conn, error) {
	con, err := nats.Connect(NatsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	return con, nil
}

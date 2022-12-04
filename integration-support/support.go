package integration_support

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
	server2 "gitlab.com/shar-workflow/shar/telemetry/server"
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
	FinalVars      map[string]interface{}
	Test           *testing.T
	Mx             sync.Mutex
	Cooldown       time.Duration
	WithTelemetry  server2.Exporter
	testTelemetry  *server2.Server
}

func init() {
	logx.SetDefault(slog.DebugLevel, false, "shar-Integration-tests")
}

//goland:noinspection GoNilness
func (s *Integration) Setup(t *testing.T) {

	s.Cooldown = 2 * time.Second
	s.Test = t
	s.FinalVars = make(map[string]interface{})
	ss, ns, err := zensvr.GetServers(NatsHost, NatsPort, 10)
	if err != nil {
		panic(err)
	}
	if s.WithTelemetry != nil {
		ctx := context.Background()
		n, err := nats.Connect(NatsURL)
		require.NoError(t, err)
		js, err := n.JetStream()
		require.NoError(t, err)
		s.testTelemetry = server2.New(ctx, js, s.WithTelemetry)
		err = s.testTelemetry.Listen()
		require.NoError(t, err)
	}
	s.testSharServer = ss
	s.testNatsServer = ns
	s.Test.Logf("\033[1;36m%s\033[0m", "> Setup completed\n")
}

func (s *Integration) AssertCleanKV() {
	time.Sleep(s.Cooldown)
	js, err := s.GetJetstream()
	require.NoError(s.Test, err)

	for n := range js.KeyValueStores() {
		name := n.Bucket()
		kvs, err := js.KeyValue(name)
		require.NoError(s.Test, err)
		keys, err := kvs.Keys()
		if err != nil && errors.Is(err, nats.ErrNoKeysFound) {
			continue
		}
		require.NoError(s.Test, err)
		switch name {
		case "WORKFLOW_DEF",
			"WORKFLOW_NAME",
			"WORKFLOW_JOB",
			"WORKFLOW_INSTANCE",
			"WORKFLOW_VERSION",
			"WORKFLOW_CLIENTTASK",
			"WORKFLOW_MSGID",
			"WORKFLOW_MSGNAME",
			"WORKFLOW_OWNERNAME",
			"WORKFLOW_OWNERID",
			"WORKFLOW_USERTASK":
			//noop
		default:
			assert.Len(s.Test, keys, 0, n.Bucket())
		}
	}

	b, err := js.KeyValue("WORKFLOW_USERTASK")
	if err != nil && errors.Is(err, nats.ErrNoKeysFound) {
		if err != nil {
			s.Test.Error(err)
			return
		}
		keys, err := b.Keys()
		if err != nil {
			s.Test.Error(err)
			return
		}
		for _, k := range keys {
			bts, err := b.Get(k)
			if err != nil {
				s.Test.Error(err)
				return
			}
			msg := &model.UserTasks{}
			err = proto.Unmarshal(bts.Value(), msg)
			if err != nil {
				s.Test.Error(err)
				return
			}
			assert.Len(s.Test, msg.Id, 0)
		}
	}
}

func (s *Integration) Teardown() {

	n, err := s.GetJetstream()
	require.NoError(s.Test, err)

	sub, err := n.PullSubscribe("WORKFLOW.>", "fin")
	require.NoError(s.Test, err)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err := sub.Fetch(1, nats.Context(ctx))
		if errors.Is(err, context.DeadlineExceeded) {
			cancel()
			break
		}
		if err != nil {
			cancel()
			s.Test.Fatal(err)
		}

		cancel()
	}
	s.Test.Log("TEARDOWN")
	s.testSharServer.Shutdown()
	s.testNatsServer.Shutdown()
	s.testNatsServer.WaitForShutdown()
	s.Test.Log("NATS shut down")
	s.Test.Logf("\033[1;36m%s\033[0m", "> Teardown completed")
	s.Test.Log("\n")
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

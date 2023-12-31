package api

import (
	"context"
	"errors"
	"fmt"
	version2 "github.com/hashicorp/go-version"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/client/api"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/version"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"time"
)

// Call provides the functionality to call shar APIs
func Call[T proto.Message, U proto.Message](ctx context.Context, con *nats.Conn, subject string, expectCompat *version2.Version, command T, ret U) error {

	b, err := proto.Marshal(command)
	if err != nil {
		return fmt.Errorf("marshal proto for call API: %w", err)
	}
	msg := nats.NewMsg(subject)
	ctx = context.WithValue(ctx, logx.CorrelationContextKey, ksuid.New().String())
	if err := header.FromCtxToMsgHeader(ctx, &msg.Header); err != nil {
		return fmt.Errorf("attach headers to outgoing API message: %w", err)
	}
	msg.Header.Add(header.NatsVersionHeader, version.Version)
	if expectCompat != nil {
		msg.Header.Add(header.NatsCompatHeader, expectCompat.String())
	} else {
		msg.Header.Add(header.NatsCompatHeader, "v0.0.0")
	}
	msg.Data = b
	res, err := con.RequestMsg(msg, time.Second*60)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			err = fmt.Errorf("shar-client: shar server is offline or missing from the current nats server")
		}
		return fmt.Errorf("API call: %w", err)
	}
	if len(res.Data) > 4 && string(res.Data[0:4]) == "ERR_" {
		em := strings.Split(string(res.Data), "_")
		e := strings.Split(em[1], "|")
		i, err := strconv.Atoi(e[0])
		if err != nil {
			i = 0
		}
		ae := &api.Error{Code: i, Message: e[1]}
		if codes.Code(i) == codes.Internal {
			return &errors2.ErrWorkflowFatal{Err: ae}
		}
		return ae
	}
	if err := proto.Unmarshal(res.Data, ret); err != nil {
		return fmt.Errorf("unmarshal proto for call API: %w", err)
	}
	return nil
}

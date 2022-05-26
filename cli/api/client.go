package api

import (
	"fmt"
	"github.com/crystal-construct/shar/model"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var Logger *zap.Logger

type Client struct {
	address string
	model.SharClient
	log *zap.Logger
}

func New(log *zap.Logger, address string) *Client {
	c := &Client{
		address: address,
		log:     log,
	}
	return c
}

func (c *Client) Dial(options ...grpc.DialOption) error {
	if len(options) == 0 {
		options = []grpc.DialOption{
			grpc.WithBlock(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStreamInterceptor(
				grpc_zap.StreamClientInterceptor(c.log),
			),
			grpc.WithUnaryInterceptor(
				grpc_zap.UnaryClientInterceptor(c.log),
			),
		}
	}
	conn, err := grpc.Dial(c.address, options...)
	if err != nil {
		return fmt.Errorf("failed to connect to grpc: %w", err)
	}
	c.SharClient = model.NewSharClient(conn)
	return nil
}

package service

import (
	"context"
	"strings"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/wire/pkt"
)

type GatewayPusher struct {
	pool *client.Pool
}

func NewGatewayPusher(pool *client.Pool) *GatewayPusher {
	return &GatewayPusher{pool: pool}
}

func (p *GatewayPusher) Push(ctx context.Context, gateway string, channels []string, packet *pkt.LogicPkt) error {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := p.pool.Get(gateway)
	if err != nil {
		return err
	}
	packetBytes := pkt.Marshal(packet)
	cli := rpc.NewGatewayServiceClient(conn)
	_, err = cli.Push(ctx, &rpc.PushReq{
		ChannelIds: strings.Join(channels, ","),
		Packet:     packetBytes,
	})
	return err
}

var _ kim.Dispatcher = (*GatewayPusher)(nil)

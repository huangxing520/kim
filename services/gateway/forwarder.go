package gateway

import (
	"context"
	"fmt"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/services/gateway/handler"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/pkt"
)

type CometForwarder struct {
	pool      *client.Pool
	selector  *handler.RouteSelector
	gatewayID string
	cfg       config.ResilienceConfig
}

func NewCometForwarder(ns naming.Naming, selector *handler.RouteSelector, gatewayID string, cfg config.ResilienceConfig, grpcCfg config.GRPCConfig) *CometForwarder {
	return &CometForwarder{
		pool:      client.NewPoolWithConfig(ns, wire.SNChat, cfg, grpcCfg),
		selector:  selector,
		gatewayID: gatewayID,
		cfg:       cfg,
	}
}

func (f *CometForwarder) Forward(ctx context.Context, p *pkt.LogicPkt) error {
	if p == nil || p.Command == "" || p.ChannelId == "" {
		return fmt.Errorf("invalid packet")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	services := f.pool.Services()
	if len(services) == 0 {
		return fmt.Errorf("no comet service available")
	}
	targetID := f.selector.Lookup(&p.Header, services)
	p.AddStringMeta(wire.MetaDestServer, f.gatewayID)
	conn, err := f.pool.Get(targetID)
	if err != nil {
		return fmt.Errorf("get comet conn for %s: %w", targetID, err)
	}
	cli := rpc.NewCometServiceClient(conn)
	_, err = cli.Forward(ctx, &rpc.ForwardReq{
		Packet: pkt.Marshal(p),
	})
	return err
}

func (f *CometForwarder) Close() {
	f.pool.Close()
}

var _ handler.Forwarder = (*CometForwarder)(nil)

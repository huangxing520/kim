package gateway

import (
	"context"
	"fmt"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/services/gateway/serv"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/pkt"
)

type CometForwarder struct {
	ns        naming.Naming
	pool      *client.Pool
	selector  *serv.RouteSelector
	gatewayID string
	cfg       config.ResilienceConfig
}

func NewCometForwarder(ns naming.Naming, selector *serv.RouteSelector, gatewayID string, cfg config.ResilienceConfig, grpcCfg config.GRPCConfig) *CometForwarder {
	return &CometForwarder{
		ns:        ns,
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
	regs, err := f.ns.Find(wire.SNChat)
	if err != nil {
		return fmt.Errorf("find comet service: %w", err)
	}
	if len(regs) == 0 {
		return fmt.Errorf("no comet service found")
	}
	services := make([]kim.Service, len(regs))
	for i, r := range regs {
		services[i] = r
	}
	targetID := f.selector.Lookup(&p.Header, services)
	p.AddStringMeta(wire.MetaDestServer, f.gatewayID)
	conn, err := f.pool.Get(targetID)
	if err != nil {
		return err
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

var _ serv.Forwarder = (*CometForwarder)(nil)

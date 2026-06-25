// 文件：forwarder.go
// 职责：Comet 转发器——封装对 CometService.Forward 的 gRPC 调用，替代原 container.Forward。
//
// 定义的类型：
//   - CometForwarder 结构体：持有 naming/selector/pool/gatewayID，实现 serv.Forwarder 接口
//
// 方法：
//   - NewCometForwarder(ns, selector, gatewayID) → 创建 CometForwarder
//   - (CometForwarder).Forward(p)                 → 查找 Comet 服务 → selector 选择 → 注入 MetaDestServer → gRPC 调用 Forward
//   - (CometForwarder).Close()                    → 关闭连接池

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

// CometForwarder Comet 转发器，封装对 CometService.Forward 的 gRPC 调用
type CometForwarder struct {
	ns        naming.Naming
	pool      *client.Pool
	selector  *serv.RouteSelector
	gatewayID string // 本 Gateway 的 ServiceID，用于注入 MetaDestServer
	cfg       config.ResilienceConfig
}

// NewCometForwarder 创建 CometForwarder
func NewCometForwarder(ns naming.Naming, selector *serv.RouteSelector, gatewayID string, cfg config.ResilienceConfig, grpcCfg config.GRPCConfig) *CometForwarder {
	return &CometForwarder{
		ns:        ns,
		pool:      client.NewPoolWithConfig(ns, wire.SNChat, cfg, grpcCfg),
		selector:  selector,
		gatewayID: gatewayID,
		cfg:       cfg,
	}
}

// Forward 转发消息到 Comet 服务（替代 container.Forward）
func (f *CometForwarder) Forward(p *pkt.LogicPkt) error {
	if p == nil || p.Command == "" || p.ChannelId == "" {
		return fmt.Errorf("invalid packet")
	}
	// 1. 查找 Comet 服务列表
	regs, err := f.ns.Find(wire.SNChat)
	if err != nil {
		return fmt.Errorf("find comet service: %w", err)
	}
	if len(regs) == 0 {
		return fmt.Errorf("no comet service found")
	}
	// 2. 转换为 []kim.Service 供 selector 使用
	services := make([]kim.Service, len(regs))
	for i, r := range regs {
		services[i] = r
	}
	// 3. 用 selector 选择目标 Comet
	targetID := f.selector.Lookup(&p.Header, services)
	// 4. 注入 MetaDestServer（告知 Comet 回推时找哪个 Gateway）
	p.AddStringMeta(wire.MetaDestServer, f.gatewayID)
	// 5. 获取 gRPC 连接并调用
	conn, err := f.pool.Get(targetID)
	if err != nil {
		return err
	}
	cli := rpc.NewCometServiceClient(conn)
	_, err = cli.Forward(context.Background(), &rpc.ForwardReq{
		Packet: pkt.Marshal(p),
	})
	return err
}

// Close 关闭连接池
func (f *CometForwarder) Close() {
	f.pool.Close()
}

// 编译时断言：CometForwarder 实现 serv.Forwarder 接口
var _ serv.Forwarder = (*CometForwarder)(nil)

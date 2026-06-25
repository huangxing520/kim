// 文件：pusher.go
// 职责：Gateway 推送器——实现 rpc.GatewayServiceServer 接口，接收 Comet 推送的消息并写入本地 channel。
//
// 定义的类型：
//   - Pusher 结构体：持有 pushFn（实际为 wsSrv.Push），实现 GatewayServiceServer
//
// 方法：
//   - NewPusher(pushFn)       → 创建 Pusher
//   - (Pusher).Push(ctx, req) → 拆分 ChannelIds → 逐个调用 pushFn 写入客户端 channel

package gateway

import (
	"context"
	"strings"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/logger"
)

// Pusher 实现 rpc.GatewayServiceServer，接收 Comet 推送的消息并写入客户端 channel
type Pusher struct {
	rpc.UnimplementedGatewayServiceServer
	pushFn func(channelID string, data []byte) error // 实际为 wsSrv.Push
}

// NewPusher 创建 Pusher
func NewPusher(pushFn func(string, []byte) error) *Pusher {
	return &Pusher{pushFn: pushFn}
}

// Push 实现 GatewayService.Push，将消息推送到指定 channel
func (p *Pusher) Push(ctx context.Context, req *rpc.PushReq) (*rpc.PushResp, error) {
	ids := strings.Split(req.ChannelIds, ",")
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := p.pushFn(id, req.Packet); err != nil {
			logger.GatewayLogger.WithField("func", "Push").Warnf("push to channel %s failed: %v", id, err)
		}
	}
	return &rpc.PushResp{Code: 0}, nil
}

// 编译时断言：Pusher 实现 rpc.GatewayServiceServer 接口
var _ rpc.GatewayServiceServer = (*Pusher)(nil)

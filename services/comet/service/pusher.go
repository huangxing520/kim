// 文件：pusher.go
// 职责：Gateway 推送器——封装 *client.Pool，实现 kim.Dispatcher 接口，
//       通过 gRPC 调用 GatewayService.Push 将消息推送到目标 gateway。
//
// 说明：
//   - Push 方法的 gateway 参数是 gateway 的 serviceID（如 "gateway-1"）
//   - 使用 pool.Get(gateway) 精确获取对应 gateway 的 gRPC 连接
//   - 使用 pkt.Marshal 序列化 LogicPkt（包含魔数 + Header + Body）

package service

import (
	"context"
	"strings"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/wire/pkt"
)

// GatewayPusher Gateway 消息推送器，实现 kim.Dispatcher 接口
type GatewayPusher struct {
	pool *client.Pool
}

// NewGatewayPusher 创建 GatewayPusher
func NewGatewayPusher(pool *client.Pool) *GatewayPusher {
	return &GatewayPusher{pool: pool}
}

// Push 将消息推送到指定 gateway 的多个 Channel
func (p *GatewayPusher) Push(gateway string, channels []string, packet *pkt.LogicPkt) error {
	conn, err := p.pool.Get(gateway)
	if err != nil {
		return err
	}
	packetBytes := pkt.Marshal(packet)
	cli := rpc.NewGatewayServiceClient(conn)
	_, err = cli.Push(context.Background(), &rpc.PushReq{
		ChannelIds: strings.Join(channels, ","),
		Packet:     packetBytes,
	})
	return err
}

// 编译时断言：GatewayPusher 实现 kim.Dispatcher 接口
var _ kim.Dispatcher = (*GatewayPusher)(nil)

// 文件：dispatcher.go
// 职责：消息分发器接口——定义如何将消息推送到指定 gateway 的一组 Channel。
//
// 定义的类型：
//   - Dispatcher 接口：将序列化后的消息包推送到指定 gateway 的多个 Channel
//
// 方法：
//   - Push(gateway, channels, packet) → 将消息推送到目标 gateway 上的指定 Channel 列表

package kim

import "github.com/klintcheng/kim/wire/pkt"

// Dispatcher 消息分发器接口，将消息推送到指定 gateway 的一组 Channel
type Dispatcher interface {
	Push(gateway string, channels []string, p *pkt.LogicPkt) error
}

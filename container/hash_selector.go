// 文件：hash_selector.go
// 职责：基于 ChannelId 哈希取模的路由选择器——将同一 Channel 的消息始终路由到同一服务节点（会话亲和性）。
//
// 定义的类型：
//   - HashSelector 结构体：Selector 接口的哈希取模实现
//
// 方法：
//   - (HashSelector).Lookup(header, srvs) → 以 header.ChannelId 的 CRC32 哈希值对服务列表长度取模，选出一个 serviceID

package container

import (
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/wire/pkt"
)

// HashSelector 基于 ChannelId 哈希取模的路由选择器
type HashSelector struct {
}

// Lookup 以 ChannelId 的哈希值对服务列表取模来选择目标节点
func (s *HashSelector) Lookup(header *pkt.Header, srvs []kim.Service) string {
	ll := len(srvs)
	if ll == 0 {
		return ""
	}
	code := HashCode(header.ChannelId)
	return srvs[code%ll].ServiceID()
}

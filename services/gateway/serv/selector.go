// 文件：selector.go
// 职责：区域路由选择器——根据消息中的 App/Account 和白名单/权重分片，将消息路由到对应 Zone 的服务节点。
//
// 定义的类型：
//   - RouteSelector 结构体：区域路由选择器（持有路由配置）
//
// 方法：
//   - NewRouteSelector(configPath)                  → 从配置文件创建 RouteSelector
//   - (RouteSelector).Lookup(header, srvs)           → 按白名单/权重选择目标服务：命中白名单→固定 Zone；否则按权重分片选 Zone→区域内哈希选节点
//   - filterSrvs(srvs, zone)                         → 过滤出指定 zone 的服务
//   - selectSrvs(srvs, account)                      → 在服务列表中按 account 哈希选择节点
//   - hashcode(key)                                  → CRC32 哈希函数

package serv

import (
	"hash/crc32"
	"math/rand"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/services/gateway/conf"
	"github.com/klintcheng/kim/wire/pkt"
)

// RouteSelector 区域路由选择器
type RouteSelector struct {
	route *conf.Route
}

func NewRouteSelector(configPath string) (*RouteSelector, error) {
	route, err := conf.ReadRoute(configPath)
	if err != nil {
		return nil, err
	}
	return &RouteSelector{
		route: route,
	}, nil
}

// Lookup a server
func (s *RouteSelector) Lookup(header *pkt.Header, srvs []kim.Service) string {
	// 1. 从header中读取Meta信息
	app, _ := pkt.FindMeta(header.Meta, MetaKeyApp)
	account, _ := pkt.FindMeta(header.Meta, MetaKeyAccount)
	if app == nil || account == nil {
		ri := rand.Intn(len(srvs))
		return srvs[ri].ServiceID()
	}
	log := logger.GatewayLogger.WithFields(logger.Fields{
		"app":     app,
		"account": account,
	})

	// 2. 判断是否命中白名单
	zone, ok := s.route.Whitelist[app.(string)]
	if !ok { // 未命中情况
		var key string
		switch s.route.RouteBy {
		case MetaKeyApp:
			key = app.(string)
		case MetaKeyAccount:
			key = account.(string)
		default:
			key = account.(string)
		}
		// 3. 通过权重计算出zone
		slot := hashcode(key) % len(s.route.Slots)
		i := s.route.Slots[slot]
		zone = s.route.Zones[i].ID
	} else {
		log.Infoln("hit a zone in whitelist", zone)
	}
	// 4. 过滤出当前zone的servers
	zoneSrvs := filterSrvs(srvs, zone)
	if len(zoneSrvs) == 0 {
		noServerFoundErrorTotal.WithLabelValues(zone).Inc()
		log.Warnf("select a random service from all due to no service found in zone %s", zone)
		ri := rand.Intn(len(srvs))
		return srvs[ri].ServiceID()
	}
	// 5. 从zoneSrvs中选中一个服务
	srv := selectSrvs(zoneSrvs, account.(string))
	return srv.ServiceID()
}

func filterSrvs(srvs []kim.Service, zone string) []kim.Service {
	var res = make([]kim.Service, 0, len(srvs))
	for _, srv := range srvs {
		if zone == srv.GetMeta()["zone"] {
			res = append(res, srv)
		}
	}
	return res
}

func selectSrvs(srvs []kim.Service, account string) kim.Service {
	// 【修复#7】原代码每次都 make([]int, 0, len(srvs)*10) 并填充 slots 切片
	// 网关消息转发是极高频率操作，每次路由查找都分配 len(srvs)*10 大小的切片造成 GC 压力
	// 由于所有服务权重相同（都是10），slots[i] 的值就是 i，等价于直接 hashcode(account) % len(srvs)
	// 新加的：直接取模，避免 slots 切片分配
	if len(srvs) == 0 {
		return nil
	}
	slot := hashcode(account) % len(srvs) // 新加的：直接取模，等价于原 slots 逻辑
	return srvs[slot]
}

func hashcode(key string) int {
	hash32 := crc32.NewIEEE()
	hash32.Write([]byte(key))
	return int(hash32.Sum32())
}

// 文件：route.go
// 职责：路由配置加载——从 JSON 文件读取区域路由配置（Zone 权重分片、白名单），生成 Slots 哈希槽。
//
// 定义的类型：
//   - Zone 结构体：区域定义（ID + 权重）
//   - Route 结构体：路由配置（RouteBy / Zones / Whitelist / Slots 权重分片）
//
// 方法：
//   - ReadRoute(path) → 从文件加载路由配置，生成权重分片 Slots 和白名单

package conf

import (
	"encoding/json"
	"os"

	"github.com/klintcheng/kim/internal/logger"
)

// Zone 区域定义（ID + 权重）
type Zone struct {
	ID     string
	Weight int
}

// Route 路由配置（含权重分片和白名单）
type Route struct {
	RouteBy   string
	Zones     []Zone
	Whitelist map[string]string
	Slots     []int
}

// ReadRoute 从 JSON 文件加载路由配置，生成权重分片
func ReadRoute(path string) (*Route, error) {
	var conf struct {
		RouteBy   string `json:"route_by,omitempty"`
		Zones     []Zone `json:"zones,omitempty"`
		Whitelist []struct {
			Key   string `json:"key,omitempty"`
			Value string `json:"value,omitempty"`
		} `json:"whitelist,omitempty"`
	}

	bts, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bts, &conf)
	if err != nil {
		return nil, err
	}

	var rt = Route{
		RouteBy:   conf.RouteBy,
		Zones:     conf.Zones,
		Whitelist: make(map[string]string, len(conf.Whitelist)),
		Slots:     make([]int, 0),
	}
	// build slots
	for i, zone := range conf.Zones {
		// 1.通过权重生成分片中的slots
		shard := make([]int, zone.Weight)
		// 2. 给当前slots设置值，指向索引i
		for j := 0; j < zone.Weight; j++ {
			shard[j] = i
		}
		// 2. 追加到Slots中
		rt.Slots = append(rt.Slots, shard...)
	}
	for _, wl := range conf.Whitelist {
		rt.Whitelist[wl.Key] = wl.Value
	}
	logger.CommonLogger.Infof("route: %+v", rt)
	return &rt, nil
}

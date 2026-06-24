// 文件：router.go
// 职责：区域路由 API——根据客户端 IP 查找所属区域，从 Naming 获取最优点网关列表并返回域名。
//
// 常量：
//   - DefaultLocation：默认地理位置（中国）
//
// 定义的类型：
//   - RouterApi 结构体：路由 API 处理器（持有 Naming / IpRegion / Router 配置）
//   - LookUpResp 结构体：路由查询响应（UTC / Location / Domains）
//
// 方法：
//   - (RouterApi).Lookup(c)         → GET /api/lookup/:token 路由查询主流程
//   - selectIdc(token, region)      → 按 token 哈希选择 Region 下的 IDC
//   - selectGateways(token, gw, n)  → 按 token 哈希选择 n 个不重复的网关

package apis

import (
	"fmt"
	"hash/crc32"
	"time"

	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/naming"
	"github.com/klintcheng/kim/services/router/conf"
	"github.com/klintcheng/kim/services/router/ipregion"
	"github.com/klintcheng/kim/wire"
)

// DefaultLocation 默认地理位置
const DefaultLocation = "中国"

// RouterApi 路由 API 处理器
type RouterApi struct {
	Naming   naming.Naming
	IpRegion ipregion.IpRegion
	Config   conf.Router
}

// LookUpResp 路由查询响应
type LookUpResp struct {
	UTC      int64    `json:"utc"`
	Location string   `json:"location"`
	Domains  []string `json:"domains"`
}

// Lookup 区域路由查询主流程
func (r *RouterApi) Lookup(c iris.Context) {
	ip := kim.RealIP(c.Request())
	token := c.Params().Get("token")

	// step 1
	var location conf.Country
	ipinfo, err := r.IpRegion.Search(ip)
	if err != nil || ipinfo.Country == "0" {
		location = DefaultLocation
	} else {
		location = conf.Country(ipinfo.Country)
	}

	// step 2
	regionId, ok := r.Config.Mapping[location]
	if !ok {
		c.StopWithError(iris.StatusForbidden, err)
		return
	}

	// step 3
	region, ok := r.Config.Regions[regionId]
	if !ok {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}

	// step 4
	idc := selectIdc(token, region)

	// step 5
	gateways, err := r.Naming.Find(wire.SNWGateway, fmt.Sprintf("IDC:%s", idc.ID))
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}

	// step 6
	hits := selectGateways(token, gateways, 3)
	domains := make([]string, len(hits))
	for i, h := range hits {
		domains[i] = h.GetMeta()["domain"]
	}

	logger.RouterLogger.WithFields(logger.Fields{
		"country":  location,
		"regionId": regionId,
		"idc":      idc.ID,
	}).Infof("lookup domain %v", domains)

	 _ = c.JSON(LookUpResp{
		UTC:      time.Now().Unix(),
		Location: string(location),
		Domains:  domains,
	})
}

func selectIdc(token string, region *conf.Region) *conf.IDC {
	slot := hashcode(token) % len(region.Slots)
	i := region.Slots[slot]
	return &region.Idcs[i]
}

func selectGateways(token string, gateways []kim.ServiceRegistration, num int) []kim.ServiceRegistration {
	if len(gateways) <= num {
		return gateways
	}
	// 【修复#7】原代码每次都 make([]int, 0, len(gateways)*10) 并填充 slots 切片
	// 路由查找是高频操作，每次分配 slots 切片造成 GC 压力
	// 由于所有网关权重相同（都是10），slots[i] 的值就是 i，等价于直接 hashcode(token) % len(gateways)
	// 新加的：直接取模确定起始位置，避免 slots 切片分配
	start := hashcode(token) % len(gateways) // 新加的：直接取模确定起始索引
	res := make([]kim.ServiceRegistration, 0, num)
	for len(res) < num {
		res = append(res, gateways[start])
		start++
		if start >= len(gateways) {
			start = 0
		}
	}
	return res
}

func hashcode(key string) int {
	hash32 := crc32.NewIEEE()
	hash32.Write([]byte(key))
	return int(hash32.Sum32())
}

// 文件：ipregion.go
// 职责：IP 地理位置查询——基于 ip2region xdb 库，根据 IP 返回国家/省/市/ISP 信息。
//
// 定义的类型：
//   - IpInfo 结构体：IP 地理信息（Country / Region / City / ISP）
//   - IpRegion 接口：IP 查询抽象（Search）
//   - Ip2region 结构体：基于 xdb 的 IpRegion 实现
//
// 方法：
//   - NewIp2region(path)        → 从 xdb 文件创建 Ip2region 查询器
//   - (Ip2region).Search(ip)    → 查询 IP 的地理位置信息

package ipregion

import (
	"strings"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// IpInfo IP 地理信息
type IpInfo struct {
	Country string
	Region  string
	City    string
	ISP     string
}

// IpRegion IP 查询接口
type IpRegion interface {
	Search(ip string) (*IpInfo, error)
}

type Ip2region struct {
	searcher *xdb.Searcher
}

func NewIp2region(path string) (IpRegion, error) {
	if path == "" {
		path = "ip2region.xdb"
	}

	// Load the entire xdb file into memory for fast queries
	cBuff, err := xdb.LoadContentFromFile(path)
	if err != nil {
		return nil, err
	}

	// Create a searcher with the buffer
	searcher, err := xdb.NewWithBuffer(xdb.IPv4, cBuff)
	if err != nil {
		return nil, err
	}

	return &Ip2region{
		searcher: searcher,
	}, nil
}

func (r *Ip2region) Search(ip string) (*IpInfo, error) {
	region, err := r.searcher.Search(ip)
	if err != nil {
		return nil, err
	}

	// Parse the region string: "国家|区域|省份|城市|ISP"
	parts := strings.Split(region, "|")
	info := &IpInfo{}
	if len(parts) >= 1 {
		info.Country = parts[0]
	}
	if len(parts) >= 2 {
		info.Region = parts[1]
	}
	if len(parts) >= 3 {
		info.City = parts[2]
	}
	if len(parts) >= 5 {
		info.ISP = parts[4]
	}

	return info, nil
}

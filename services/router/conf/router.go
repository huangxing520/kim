// 文件：router.go
// 职责：路由数据配置加载——从 JSON 文件加载区域映射（国家 → Region）和区域权重分片（Region → IDC）。
//
// 定义的类型：
//   - IDC 结构体：机房定义（ID + Weight）
//   - Region 结构体：区域定义（ID / Idcs 列表 / Slots 权重分片）
//   - Country 类型：国家/地区名称
//   - Mapping 结构体：区域映射条目（Region + 所属国家/地区列表）
//   - Router 结构体：路由数据（国家→Region 映射 + Region→IDC 映射及权重）
//
// 方法：
//   - LoadMapping(path)  → 加载 mapping.json，构建 Country→Region 映射
//   - LoadRegions(path)  → 加载 regions.json，构建 Region→IDC 映射并生成权重分片 Slots

package conf

import (
	"encoding/json"
	"io/ioutil"
)

// IDC 机房定义
type IDC struct {
	ID     string
	Weight int
}

// Region 区域定义（含 IDC 权重分片）
type Region struct {
	ID    string
	Idcs  []IDC
	Slots []byte
}

// Country 国家/地区名称
type Country string

// Mapping 区域映射条目
type Mapping struct {
	Region    string
	Locations []string
}

// Router 路由数据
type Router struct {
	Mapping map[Country]string
	Regions map[string]*Region
}

// LoadMapping 加载国家→Region 映射
func LoadMapping(path string) (map[Country]string, error) {
	bts, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mps []Mapping
	err = json.Unmarshal(bts, &mps)
	if err != nil {
		return nil, err
	}
	mp := make(map[Country]string)
	for _, v := range mps {
		region := v.Region
		for _, loc := range v.Locations {
			mp[Country(loc)] = region
		}
	}
	return mp, nil
}

func LoadRegions(path string) (map[string]*Region, error) {
	bts, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var regions []*Region
	err = json.Unmarshal(bts, &regions)
	if err != nil {
		return nil, err
	}
	res := make(map[string]*Region)
	for _, region := range regions {
		res[region.ID] = region
		for i, idc := range region.Idcs {
			// 1.通过权重生成分片中的slots
			shard := make([]byte, idc.Weight)
			// 2. 给当前slots设置值，指向索引i
			for j := 0; j < idc.Weight; j++ {
				shard[j] = byte(i)
			}
			// 2. 追加到Slots中
			region.Slots = append(region.Slots, shard...)
		}
	}
	return res, nil
}

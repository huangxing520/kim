package handler

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"time"

	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"github.com/klintcheng/kim/services/router/conf"
	"github.com/klintcheng/kim/services/router/ipregion"
	"github.com/klintcheng/kim/wire"
)

const DefaultLocation = "中国"

type RouterApi struct {
	Naming   naming.Naming
	IpRegion ipregion.IpRegion
	Config   conf.Router
}

type LookUpResp struct {
	UTC      int64    `json:"utc"`
	Location string   `json:"location"`
	Domains  []string `json:"domains"`
}

func (r *RouterApi) Lookup(w http.ResponseWriter, req *http.Request) {
	ip := kim.RealIP(req)
	token := req.PathValue("token")

	var location conf.Country
	ipinfo, err := r.IpRegion.Search(ip)
	if err != nil || ipinfo.Country == "0" {
		location = DefaultLocation
	} else {
		location = conf.Country(ipinfo.Country)
	}

	regionId, ok := r.Config.Mapping[location]
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	region, ok := r.Config.Regions[regionId]
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	idc := selectIdc(token, region)

	gateways, err := r.Naming.Find(wire.SNWGateway, fmt.Sprintf("IDC:%s", idc.ID))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(LookUpResp{
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
	start := hashcode(token) % len(gateways)
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

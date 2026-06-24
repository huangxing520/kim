// 文件：metrics.go
// 职责：Prometheus 监控指标定义——网关并发连接数统计。
//
// 定义的指标：
//   - channelTotalGauge：GaugeVec，按 serviceId 和 serviceName 维度统计当前活跃的 Channel 连接数

package kim

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// channelTotalGauge 按 serviceId 和 serviceName 维度统计当前活跃连接数
var channelTotalGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "kim",
	Name:      "channel_total",
	Help:      "网关并发数",
}, []string{"serviceId", "serviceName"})

// 文件：metrics.go
// 职责：Prometheus 监控指标定义——网关下发的消息字节数统计。
//
// 定义的指标：
//   - messageOutFlowBytes：CounterVec，按消息 command 维度统计网关下发的消息字节总数

package container

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// messageOutFlowBytes 按 command 维度统计网关下发的消息字节数
var messageOutFlowBytes = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "kim",
	Name:      "message_out_flow_bytes",
	Help:      "网关下发的消息字节数",
}, []string{"command"})

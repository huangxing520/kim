// 文件：metrics.go
// 职责：Gateway 监控指标定义——网关接收消息的总数和字节数，以及 Zone 服务查找失败次数。
//
// 定义的指标：
//   - messageInTotal：CounterVec，按 serviceId/serviceName/command 维度统计网关接收消息总数
//   - messageInFlowBytes：CounterVec，按同维度统计网关接收消息字节总数
//   - noServerFoundErrorTotal：CounterVec，按 zone 维度统计查找分区服务失败的次数

package handler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// messageInTotal 按维度统计网关接收消息总数
var messageInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "kim",
	Name:      "message_in_total",
	Help:      "网关接收消息总数",
}, []string{"serviceId", "serviceName", "command"})

var messageInFlowBytes = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "kim",
	Name:      "message_in_flow_bytes",
	Help:      "网关接收消息字节数",
}, []string{"serviceId", "serviceName", "command"})

var noServerFoundErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "kim",
	Name:      "no_server_found_error_total",
	Help:      "查找zone分区中服务失败的次数",
}, []string{"zone"})

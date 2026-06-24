// 文件：dialer.go
// 职责：Gateway TCP 拨号器——与其他服务建立 TCP 连接时发送 InnerHandshakeReq 握手包。
//
// 定义的类型：
//   - TcpDialer 结构体：TCP 拨号器（持有本服务 ServiceId，握手时发送给对端）
//
// 方法：
//   - NewDialer(serviceId)                            → 创建 TcpDialer
//   - (TcpDialer).DialAndHandshake(ctx)                → TCP 拨号后发送 InnerHandshakeReq（告知对端本服务 ID）

package serv

import (
	"net"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/tcp"
	"github.com/klintcheng/kim/wire/pkt"
	"google.golang.org/protobuf/proto"
)

// TcpDialer TCP 拨号器
type TcpDialer struct {
	ServiceId string
}

// NewDialer 创建 TcpDialer
func NewDialer(serviceId string) kim.Dialer {
	return &TcpDialer{
		ServiceId: serviceId,
	}
}

// DialAndHandshake TCP 拨号后发送握手包（告知对端本服务 ID）
func (d *TcpDialer) DialAndHandshake(ctx kim.DialerContext) (net.Conn, error) {
	// 1. 拨号建立连接
	conn, err := net.DialTimeout("tcp", ctx.Address, ctx.Timeout)
	if err != nil {
		return nil, err
	}
	req := &pkt.InnerHandshakeReq{
		ServiceId: d.ServiceId,
	}
	logger.GatewayLogger.WithField("func", "DialAndHandshake").Infof("send req %v", req)
	// 2. 把自己的ServiceId发送给对方
	bts, _ := proto.Marshal(req)
	err = tcp.WriteFrame(conn, kim.OpBinary, bts)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

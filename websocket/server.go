// 文件：server.go
// 职责：WebSocket 服务端工厂——创建基于 WebSocket 协议的 Server 实例，包含 WebSocket 升级器。
//
// 定义的类型：
//   - Upgrader 结构体：WebSocket 协议升级器，实现 kim.Upgrader 接口
//
// 方法：
//   - NewServer(listen, service, options...) → 创建一个 WebSocket 协议的 Server 实例
//   - (Upgrader).Name()                       → 返回协议名称 "websocket.Server"
//   - (Upgrader).Upgrade(rawconn, rd, wr)     → 执行 WebSocket 握手升级，返回包装后的 WsConn

package websocket

import (
	"bufio"
	"net"

	"github.com/gobwas/ws"
	kim "github.com/klintcheng/kim/internal/kim"
)

// Upgrader WebSocket 协议升级器
type Upgrader struct {
}

// NewServer 创建一个 WebSocket 协议的 Server 实例
func NewServer(listen string, service kim.ServiceRegistration, options ...kim.ServerOption) kim.Server {
	return kim.NewServer(listen, service, new(Upgrader), options...)
}

// Name 返回协议名称
func (u *Upgrader) Name() string {
	return "websocket.Server"
}

// Upgrade 执行 WebSocket 握手升级，返回 WsConn
func (u *Upgrader) Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (kim.Conn, error) {
	_, err := ws.Upgrade(rawconn)
	if err != nil {
		return nil, err
	}

	conn := NewConnWithRW(rawconn, rd, wr)
	return conn, nil
}

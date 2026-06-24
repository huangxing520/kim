// 文件：server.go
// 职责：TCP 服务端工厂——创建基于自定义 TCP 帧协议的 Server 实例，包含 TCP 升级器。
//
// 定义的类型：
//   - Upgrader 结构体：TCP 协议升级器，实现 kim.Upgrader 接口（TCP 无需握手，直接包装为 TcpConn）
//
// 方法：
//   - NewServer(listen, service, options...) → 创建一个 TCP 协议的 Server 实例
//   - (Upgrader).Name()                       → 返回协议名称 "tcp.Server"
//   - (Upgrader).Upgrade(rawconn, rd, wr)     → 直接将 rawconn 包装为带缓冲的 TcpConn（无握手）

package tcp

import (
	"bufio"
	"net"

	"github.com/klintcheng/kim"
)

// Upgrader TCP 协议升级器（无握手，直接包装）
type Upgrader struct {
}

// NewServer 创建一个 TCP 协议的 Server 实例
func NewServer(listen string, service kim.ServiceRegistration, options ...kim.ServerOption) kim.Server {
	return kim.NewServer(listen, service, new(Upgrader), options...)
}

// Name 返回协议名称
func (u *Upgrader) Name() string {
	return "tcp.Server"
}

// Upgrade 直接将 rawconn 包装为 TcpConn（TCP 无握手协议）
func (u *Upgrader) Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (kim.Conn, error) {
	conn := NewConnWithRW(rawconn, rd, wr)
	return conn, nil
}

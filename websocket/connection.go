// 文件：connection.go
// 职责：WebSocket 连接和帧实现——封装 WebSocket 帧（Frame）和带缓冲的连接（WsConn）。
//
// 定义的类型：
//   - Frame 结构体：WebSocket 帧包装，封装 gobwas/ws.Frame
//   - WsConn 结构体：带 bufio 缓冲的 WebSocket 连接，实现 kim.Conn 接口
//
// 方法：
//   - (Frame).SetOpCode / GetOpCode / SetPayload / GetPayload → 帧操作码和负载的读写
//   - NewConn(conn)                           → 创建一个带缓冲的 WsConn（默认读写缓冲）
//   - NewConnWithRW(conn, rd, wr)             → 使用指定读写缓冲创建 WsConn
//   - (WsConn).ReadFrame()                    → 从 bufio.Reader 读取一帧
//   - (WsConn).WriteFrame(code, payload)      → 向 bufio.Writer 写入一帧（批量 Flush 优化）
//   - (WsConn).Flush()                        → 刷新缓冲写入

package websocket

import (
	"bufio"
	"github.com/gobwas/ws/wsutil"
	"net"

	"github.com/gobwas/ws"
	kim "github.com/klintcheng/kim/internal/kim"
)

// Frame WebSocket 帧包装
type Frame struct {
	raw ws.Frame
}

func (f *Frame) SetOpCode(code kim.OpCode) {
	f.raw.Header.OpCode = ws.OpCode(code)
}

func (f *Frame) GetOpCode() kim.OpCode {
	return kim.OpCode(f.raw.Header.OpCode)
}

func (f *Frame) SetPayload(payload []byte) {
	f.raw.Payload = payload
}

func (f *Frame) GetPayload() []byte {
	if f.raw.Header.Masked {
		ws.Cipher(f.raw.Payload, f.raw.Header.Mask, 0)
	}
	f.raw.Header.Masked = false
	return f.raw.Payload
}

type WsConn struct {
	net.Conn
	rd *bufio.Reader
	wr *bufio.Writer
}

func NewConn(conn net.Conn) kim.Conn {
	return &WsConn{
		Conn: conn,
		rd:   bufio.NewReaderSize(conn, 4096),
		// 【修复#16】原代码 wr: bufio.NewWriterSize(conn, 1024) 写缓冲区过小
		// 1024 字节的写缓冲区会导致稍大的消息体就触发系统调用
		// 新加的：扩大写缓冲区到 8192，减少系统调用次数
		wr: bufio.NewWriterSize(conn, 8192), // 新加的：从 1024 扩大到 8192
	}
}

func NewConnWithRW(conn net.Conn, rd *bufio.Reader, wr *bufio.Writer) *WsConn {
	return &WsConn{
		Conn: conn,
		rd:   rd,
		wr:   wr,
	}
}

func (c *WsConn) ReadFrame() (kim.Frame, error) {
	f, err := ws.ReadFrame(c.rd)
	if err != nil {
		return nil, err
	}
	return &Frame{raw: f}, nil
}

func (c *WsConn) WriteFrame(code kim.OpCode, payload []byte) error {
	//f := ws.NewFrame(ws.OpCode(code), true, payload)
	//return ws.WriteFrame(c.wr, f)
	// 【修复#15】原代码 return wsutil.WriteServerMessage(c, ws.OpCode(code), payload)
	// wsutil.WriteServerMessage 直接写入 net.Conn（c），绕过了 bufio.Writer 缓冲区
	// 导致每条消息都触发一次系统调用，无法利用 writeloop 中的批量 Flush
	// 新加的：改为写入缓冲区 c.wr，由 writeloop 统一 Flush，减少系统调用次数
	return wsutil.WriteServerMessage(c.wr, ws.OpCode(code), payload) // 新加的：写入缓冲区而非直连
}

func (c *WsConn) Flush() error {
	return c.wr.Flush()
}

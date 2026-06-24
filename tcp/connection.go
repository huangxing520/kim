// 文件：connection.go
// 职责：TCP 连接和帧实现——封装自定义 TCP 帧协议（OpCode + Payload）和带缓冲的连接（TcpConn）。
//
// 定义的类型：
//   - Frame 结构体：TCP 帧，包含 OpCode 和 Payload
//   - TcpConn 结构体：带 bufio 缓冲的 TCP 连接，实现 kim.Conn 接口
//
// 方法：
//   - (Frame).SetOpCode / GetOpCode / SetPayload / GetPayload → 帧操作码和负载的读写
//   - NewConn(conn)                           → 创建一个带缓冲的 TcpConn（默认读写缓冲）
//   - NewConnWithRW(conn, rd, wr)             → 使用指定读写缓冲创建 TcpConn
//   - (TcpConn).ReadFrame()                   → 从 bufio.Reader 读取帧（1字节 OpCode + 长度前缀 Payload）
//   - (TcpConn).WriteFrame(code, payload)     → 向 bufio.Writer 写入帧
//   - (TcpConn).Flush()                       → 刷新缓冲写入
//   - WriteFrame(w, code, payload)            → 通用帧写入函数：OpCode + 长度前缀 + Payload

package tcp

import (
	"bufio"
	"io"
	"net"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/wire/endian"
)

// Frame TCP 自定义帧（OpCode + Payload）
type Frame struct {
	OpCode  kim.OpCode
	Payload []byte
}

// SetOpCode SetOpCode
func (f *Frame) SetOpCode(code kim.OpCode) {
	f.OpCode = code
}

// GetOpCode GetOpCode
func (f *Frame) GetOpCode() kim.OpCode {
	return f.OpCode
}

// SetPayload SetPayload
func (f *Frame) SetPayload(payload []byte) {
	f.Payload = payload
}

// GetPayload GetPayload
func (f *Frame) GetPayload() []byte {
	return f.Payload
}

// TcpConn Conn
type TcpConn struct {
	net.Conn
	rd *bufio.Reader
	wr *bufio.Writer
}

// NewConn NewConn

func NewConn(conn net.Conn) kim.Conn {
	return &TcpConn{
		Conn: conn,
		rd:   bufio.NewReaderSize(conn, 4096),
		// 【修复#16】原代码 wr: bufio.NewWriterSize(conn, 1024) 写缓冲区过小
		// 1024 字节的写缓冲区会导致稍大的消息体就触发系统调用
		// 新加的：扩大写缓冲区到 8192，减少系统调用次数
		wr: bufio.NewWriterSize(conn, 8192), // 新加的：从 1024 扩大到 8192
	}
}

func NewConnWithRW(conn net.Conn, rd *bufio.Reader, wr *bufio.Writer) *TcpConn {
	return &TcpConn{
		Conn: conn,
		rd:   rd,
		wr:   wr,
	}
}

// ReadFrame ReadFrame
func (c *TcpConn) ReadFrame() (kim.Frame, error) {
	opcode, err := endian.ReadUint8(c.rd)
	if err != nil {
		return nil, err
	}
	payload, err := endian.ReadBytes(c.rd)
	if err != nil {
		return nil, err
	}
	return &Frame{
		OpCode:  kim.OpCode(opcode),
		Payload: payload,
	}, nil
}

// WriteFrame WriteFrame
func (c *TcpConn) WriteFrame(code kim.OpCode, payload []byte) error {
	return WriteFrame(c.wr, code, payload)
}

// Flush Flush
func (c *TcpConn) Flush() error {
	return c.wr.Flush()
}

// WriteFrame write a frame to w
func WriteFrame(w io.Writer, code kim.OpCode, payload []byte) error {
	if err := endian.WriteUint8(w, uint8(code)); err != nil {
		return err
	}
	if err := endian.WriteBytes(w, payload); err != nil {
		return err
	}
	return nil
}

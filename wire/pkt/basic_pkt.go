// 文件：basic_pkt.go
// 职责：BasicPkt 基础包定义——轻量的 Ping/Pong 心跳包，固定 4 字节头（Code + Length）+ 可变 Body。
//
// 常量：
//   - CodePing / CodePong：心跳包操作码
//
// 定义的类型：
//   - BasicPkt 结构体：基础包（Code uint16 + Length uint16 + Body）
//
// 方法：
//   - (BasicPkt).Decode(r) → 从 io.Reader 解码 BasicPkt
//   - (BasicPkt).Encode(w) → 将 BasicPkt 编码到 io.Writer

package pkt

import (
	"io"

	"github.com/klintcheng/kim/wire/endian"
)

// 心跳包操作码
const (
	CodePing = uint16(1)
	CodePong = uint16(2)
)

// BasicPkt 轻量基础包（Ping/Pong 用）
type BasicPkt struct {
	Code   uint16
	Length uint16
	Body   []byte
}

// Decode 从 io.Reader 解码 BasicPkt
func (p *BasicPkt) Decode(r io.Reader) error {
	var err error
	if p.Code, err = endian.ReadUint16(r); err != nil {
		return err
	}
	if p.Length, err = endian.ReadUint16(r); err != nil {
		return err
	}
	if p.Length > 0 {
		if p.Body, err = endian.ReadFixedBytes(int(p.Length), r); err != nil {
			return err
		}
	}
	return nil
}

func (p *BasicPkt) Encode(w io.Writer) error {
	if err := endian.WriteUint16(w, p.Code); err != nil {
		return err
	}
	if err := endian.WriteUint16(w, p.Length); err != nil {
		return err
	}
	if p.Length > 0 {
		if _, err := w.Write(p.Body); err != nil {
			return err
		}
	}
	return nil
}

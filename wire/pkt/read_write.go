// 文件：read_write.go
// 职责：消息包序列化/反序列化——通过魔数识别包类型（LogicPkt vs BasicPkt），提供通用的 Read/Marshal 函数。
//
// 定义的类型：
//   - Packet 接口：可编解码的消息包抽象（Decode / Encode）
//
// 方法：
//   - MustReadLogicPkt(r)  → 从 io.Reader 读取并断言为 LogicPkt
//   - MustReadBasicPkt(r)  → 从 io.Reader 读取并断言为 BasicPkt
//   - Read(r)              → 通用读取：先读魔数，再根据魔数分发到 LogicPkt 或 BasicPkt 解码
//   - Marshal(p)           → 通用序列化：根据类型写入魔数 + 调用 Encode

package pkt

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/klintcheng/kim/wire"
)

// Packet 可编解码的消息包接口
type Packet interface {
	Decode(r io.Reader) error
	Encode(w io.Writer) error
}

// MustReadLogicPkt 读取并断言为 LogicPkt
func MustReadLogicPkt(r io.Reader) (*LogicPkt, error) {
	val, err := Read(r)
	if err != nil {
		return nil, err
	}
	if lp, ok := val.(*LogicPkt); ok {
		return lp, nil
	}
	return nil, fmt.Errorf("packet is not a logic packet")
}

func MustReadBasicPkt(r io.Reader) (*BasicPkt, error) {
	val, err := Read(r)
	if err != nil {
		return nil, err
	}
	if bp, ok := val.(*BasicPkt); ok {
		return bp, nil
	}
	return nil, fmt.Errorf("packet is not a basic packet")
}

func Read(r io.Reader) (interface{}, error) {
	magic := wire.Magic{}
	_, err := io.ReadFull(r, magic[:])
	if err != nil {
		return nil, err
	}
	switch magic {
	case wire.MagicLogicPkt:
		p := new(LogicPkt)
		if err := p.Decode(r); err != nil {
			return nil, err
		}
		return p, nil
	case wire.MagicBasicPkt:
		p := new(BasicPkt)
		if err := p.Decode(r); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("magic code %s is incorrect", magic)
	}
}

func Marshal(p Packet) []byte {
	buf := new(bytes.Buffer)
	kind := reflect.TypeOf(p).Elem()

	if kind.AssignableTo(reflect.TypeOf(LogicPkt{})) {
		_, _ = buf.Write(wire.MagicLogicPkt[:])
	} else if kind.AssignableTo(reflect.TypeOf(BasicPkt{})) {
		_, _ = buf.Write(wire.MagicBasicPkt[:])
	}
	_ = p.Encode(buf)
	return buf.Bytes()
}

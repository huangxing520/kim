package kim

import (
	"bytes" // 【修复#9】保留 bytes 导入，Unmarshal 仍需使用
	"errors"

	"github.com/klintcheng/kim/wire/endian"
)

type Location struct {
	ChannelId string
	GateId    string
}

func (loc *Location) Bytes() []byte {
	if loc == nil {
		return []byte{}
	}
	// 【修复#9】原代码 buf := new(bytes.Buffer) 每次调用都分配新的 bytes.Buffer
	// 在登录等热路径上频繁分配，增加 GC 压力
	// 新加的：预分配定长字节数组，避免 bytes.Buffer 分配
	// 格式：2字节长度 + ChannelId + 2字节长度 + GateId
	channelBytes := []byte(loc.ChannelId) // 新加的：转换为字节切片
	gateBytes := []byte(loc.GateId)       // 新加的：转换为字节切片
	// 新加的：预分配足够大的缓冲区：4字节头 + 两个字符串长度
	buf := make([]byte, 0, 4+len(channelBytes)+len(gateBytes)) // 新加的：预分配
	// 新加的：手动写入长度前缀 + 内容，等价于 endian.WriteShortBytes
	buf = appendShortBytes(buf, channelBytes) // 新加的：写入 ChannelId
	buf = appendShortBytes(buf, gateBytes)    // 新加的：写入 GateId
	return buf
}

// 新加的：appendShortBytes 将长度前缀（2字节大端）和数据追加到 buf
// 等价于 endian.WriteShortBytes 但避免 bytes.Buffer 分配
func appendShortBytes(buf, data []byte) []byte {
	length := len(data)
	buf = append(buf, byte(length>>8), byte(length)) // 新加的：2字节大端长度
	buf = append(buf, data...)                       // 新加的：数据内容
	return buf
}

func (loc *Location) Unmarshal(data []byte) (err error) {
	if len(data) == 0 {
		return errors.New("data is empty")
	}
	// 【修复#9】保留原 bytes.NewBuffer 方式，因为 Unmarshal 不在热路径上
	buf := bytes.NewBuffer(data) // 保留原实现，Unmarshal 不在热路径
	loc.ChannelId, err = endian.ReadShortString(buf)
	if err != nil {
		return
	}
	loc.GateId, err = endian.ReadShortString(buf)
	if err != nil {
		return
	}
	return
}

// 文件：seq.go
// 职责：全局序列号生成器——线程安全的递增序列号，用于消息 Header 的 Sequence 字段。
//
// 方法：
//   - (sequence).Next() → 原子递增并返回下一个序列号（溢出后回绕到 1）
//
// 全局变量：
//   - Seq：全局序列号实例，初始值 1

package wire

import (
	"math"
	"sync/atomic"
)

// sequence 线程安全的递增序列号
type sequence struct {
	num uint32
}

// Next 原子递增并返回下一个序列号
func (s *sequence) Next() uint32 {
	next := atomic.AddUint32(&s.num, 1)
	if next == math.MaxUint32 {
		if atomic.CompareAndSwapUint32(&s.num, next, 1) {
			return 1
		}
		return s.Next()
	}
	return next
}

// Seq 全局序列号实例
var Seq = sequence{num: 1}

// 文件：event.go
// 职责：一次性事件——线程安全的一次性信号通知机制，可用于等待某个操作完成。
//
// 定义的类型：
//   - Event 结构体：一次性事件，Fire 后 Done() 通道关闭，可安全并发调用
//
// 方法：
//   - NewEvent()                             → 创建一个新的 Event 实例
//   - (Event).Fire()                         → 触发事件，关闭 Done 通道（通过 sync.Once 保证仅执行一次）
//   - (Event).Done()                         → 返回一个 channel，事件触发后该 channel 会被关闭
//   - (Event).HasFired()                     → 检查事件是否已被触发

package kim

import (
	"sync"
	"sync/atomic"
)

// Event 一次性事件，线程安全的一次性信号通知机制
type Event struct {
	fired int32
	c     chan struct{}
	o     sync.Once
}

// Fire causes e to complete.  It is safe to call multiple times, and
// concurrently.  It returns true iff this call to Fire caused the signaling
// channel returned by Done to close.
func (e *Event) Fire() bool {
	ret := false
	e.o.Do(func() {
		atomic.StoreInt32(&e.fired, 1)
		close(e.c)
		ret = true
	})
	return ret
}

// Done returns a channel that will be closed when Fire is called.
func (e *Event) Done() <-chan struct{} {
	return e.c
}

// HasFired returns true if Fire has been called.
func (e *Event) HasFired() bool {
	return atomic.LoadInt32(&e.fired) == 1
}

// NewEvent returns a new, ready-to-use Event.
func NewEvent() *Event {
	return &Event{c: make(chan struct{})}
}

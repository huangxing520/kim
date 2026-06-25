package kim

import (
	"sync"
	"testing"
)

// newTestChannel 构造一个不启动 writeloop 的 ChannelImpl 用于 race 测试
func newTestChannel() *ChannelImpl {
	return &ChannelImpl{
		id:        "test",
		writechan: make(chan []byte, 32),
		closeChan: make(chan struct{}),
		writeWait: DefaultWriteWait,
		readwait:  DefaultReadWait,
		state:     1, // 模拟已 Readloop 启动（Close CAS 1→2 才能成功）
	}
}

// TestPushCloseNoPanic 验证并发 Push + Close 不会触发 send on closed channel panic
func TestPushCloseNoPanic(t *testing.T) {
	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := newTestChannel()
			var pwg sync.WaitGroup
			// 2 个 Push goroutine
			for j := 0; j < 2; j++ {
				pwg.Add(1)
				go func() {
					defer pwg.Done()
					_ = ch.Push([]byte("hello"))
				}()
			}
			// 1 个 Close goroutine
			pwg.Add(1)
			go func() {
				defer pwg.Done()
				_ = ch.Close()
			}()
			pwg.Wait()
		}()
	}
	wg.Wait()
}

// TestPushAfterCloseReturnsError 验证 Push 在 Close 之后返回 error 而非 panic
func TestPushAfterCloseReturnsError(t *testing.T) {
	ch := newTestChannel()
	_ = ch.Close()
	// Close 后 state=2，Push 的 atomic 检查会直接返回 error
	err := ch.Push([]byte("x"))
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

// TestCloseIdempotent 验证重复 Close 不会 panic（close of closed channel）
func TestCloseIdempotent(t *testing.T) {
	ch := newTestChannel()
	_ = ch.Close()
	// 第二次 Close CAS 失败，返回 error，不 panic
	err := ch.Close()
	if err == nil {
		t.Error("expected error on second Close, got nil")
	}
}

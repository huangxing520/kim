// 文件：channel.go
// 职责：Channel 连接实现——单个客户端连接的完整抽象，管理连接的读写生命周期。
//
// 定义的类型：
//   - ChannelImpl 结构体：Channel 接口的实现，封装 Conn 连接、元数据、读写缓冲区和协程池
//
// 方法：
//   - NewChannel(id, meta, conn, gpool)      → 创建新 Channel，启动后台写循环
//   - (ChannelImpl).ID()                      → 获取 Channel 的唯一标识
//   - (ChannelImpl).Conn()                    → 获取底层网络连接
//   - (ChannelImpl).RemoteAddr()              → 获取对端地址
//   - (ChannelImpl).Readloop(listener)        → 读循环：从 Conn 读帧 → 回调 MessageListener.Receive → 协程池处理
//   - (ChannelImpl).writeloop()               → 写循环：从 writechan 取数据 → WriteFrame → Flush
//   - (ChannelImpl).WriteFrame(code, payload) → 写一帧到 writechan（非阻塞）或直接写（阻塞）
//   - (ChannelImpl).Close()                   → 关闭 Channel（设置关闭状态 + close writechan）
//   - (ChannelImpl).SetReadWait(duration)     → 设置读超时
//   - (ChannelImpl).SetWriteWait(duration)    → 设置写超时

package kim

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/panjf2000/ants/v2"
)

// ChannelImpl Channel 接口的完整实现
type ChannelImpl struct {
	id string
	Conn
	meta      Meta
	writechan chan []byte
	writeWait time.Duration
	readwait  time.Duration
	gpool     *ants.Pool
	state     int32 // 0 init 1 start 2 closed
}

// NewChannel NewChannel
func NewChannel(id string, meta Meta, conn Conn, gpool *ants.Pool) Channel {
	ch := &ChannelImpl{
		id:   id,
		Conn: conn,
		meta: meta,
		// 【修复#8】原代码 writechan: make(chan []byte, 5) 缓冲区过小
		// 当服务端推送速度超过客户端接收速度时，Push 会阻塞，进而阻塞调用方
		// 群聊消息风暴场景容易触发背压
		// 新加的：扩大写缓冲区到 32，抗突发流量
		writechan: make(chan []byte, 32), // 新加的：缓冲区从 5 扩大到 32
		writeWait: DefaultWriteWait,      //default value
		readwait:  DefaultReadWait,
		gpool:     gpool,
		state:     0,
	}
	go func() {
		err := ch.writeloop()
		if err != nil {
			logger.CommonLogger.WithFields(logger.Fields{
				"module": "ChannelImpl",
				"id":     id,
			}).Info(err)
		}
	}()
	return ch
}

func (ch *ChannelImpl) writeloop() error {
	log := logger.CommonLogger.WithFields(logger.Fields{
		"module": "ChannelImpl",
		"func":   "writeloop",
		"id":     ch.id,
	})
	defer func() {
		log.Debugf("channel %s writeloop exited", ch.id)
	}()
	for payload := range ch.writechan {
		err := ch.WriteFrame(OpBinary, payload)
		if err != nil {
			return err
		}
		// 【修复#15】批量取出缓冲区内的剩余消息，统一 Flush 减少系统调用次数
		// WriteFrame 写入 bufio.Writer 缓冲区，Flush 才真正发送到网络
		flushed := false
		for !flushed {
			select {
			case payload, ok := <-ch.writechan:
				if !ok {
					// channel 已关闭，写入已缓冲的数据后退出
					return ch.Flush()
				}
				err := ch.WriteFrame(OpBinary, payload)
				if err != nil {
					return err
				}
			default:
				// 缓冲区已排空，统一 Flush
				if err := ch.Flush(); err != nil {
					return err
				}
				flushed = true
			}
		}
	}
	return nil
}

// ID id simpling server
func (ch *ChannelImpl) ID() string { return ch.id }

// Push 异步写数据
func (ch *ChannelImpl) Push(payload []byte) error {
	if atomic.LoadInt32(&ch.state) != 1 {
		return fmt.Errorf("channel %s has closed", ch.id)
	}
	// 异步写
	ch.writechan <- payload
	return nil
}

// Close 关闭连接
func (ch *ChannelImpl) Close() error {
	if !atomic.CompareAndSwapInt32(&ch.state, 1, 2) {
		return fmt.Errorf("channel has started")
	}
	close(ch.writechan)
	return nil
}

// SetWriteWait 设置写超时
func (ch *ChannelImpl) SetWriteWait(writeWait time.Duration) {
	if writeWait == 0 {
		return
	}
	ch.writeWait = writeWait
}

func (ch *ChannelImpl) SetReadWait(readwait time.Duration) {
	if readwait == 0 {
		return
	}
	ch.readwait = readwait
}

// Declare a function called Readloop that belongs to the ChannelImpl struct.
// This function takes in a MessageListener as a parameter and returns an error (if there is one).
func (ch *ChannelImpl) Readloop(lst MessageListener) error {
	// Perform an atomic compare-and-swap operation on ch.state. If the current value is 0, set it to 1.
	// If it's already 1, return an error indicating that the channel has already started.
	if !atomic.CompareAndSwapInt32(&ch.state, 0, 1) {
		return fmt.Errorf("channel has started")
	}

	// Create a new logger object with some fields filled out.
	log := logger.CommonLogger.WithFields(logger.Fields{
		"struct": "ChannelImpl",
		"func":   "Readloop",
		"id":     ch.id,
	})

	// Start an infinite loop.
	for {
		// Set a read deadline for the channel.
		_ = ch.SetReadDeadline(time.Now().Add(ch.readwait))

		// Attempt to read a frame from the channel.
		frame, err := ch.ReadFrame()
		if err != nil {
			// If reading the frame failed, log the error and return it.
			log.Info(err)
			return err
		}
		if frame.GetOpCode() == OpClose {
			// If the received frame has an OpCode of OpClose, return an error indicating that the remote side closed the channel.
			return errors.New("remote side closed the channel")
		}
		if frame.GetOpCode() == OpPing {
			// If the received frame has an OpCode of OpPing, log that we received a ping and respond with a pong.
			log.Trace("recv a ping; resp with a pong")

			_ = ch.WriteFrame(OpPong, nil)
			_ = ch.Flush()
			continue
		}
		payload := frame.GetPayload()
		if len(payload) == 0 {
			// If the payload is empty, skip to the next iteration of the loop.
			continue
		}
		err = ch.gpool.Submit(func() {
			// Submit a new task to the gpool (which is an instance of a goroutine pool).
			// This task calls lst.Receive with the channel and payload as parameters.
			lst.Receive(ch, payload)
		})
		if err != nil {
			// If submitting the task to the gpool failed, return an error.
			return err
		}
	}
}

func (ch *ChannelImpl) GetMeta() Meta { return ch.meta }

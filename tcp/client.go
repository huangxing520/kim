// 文件：client.go
// 职责：TCP 客户端实现——基于自定义 TCP 帧协议的客户端，支持连接、读写、心跳、关闭。
//
// 定义的类型：
//   - ClientOptions 结构体：客户端配置（心跳间隔、读写超时）
//   - Client 结构体：TCP 客户端实现，组合 kim.Dialer，管理连接状态和读写
//
// 方法：
//   - NewClient(id, name, opts)                       → 创建一个 TCP 客户端（空 Meta）
//   - NewClientWithProps(id, name, meta, opts)         → 创建一个带 Meta 的 TCP 客户端
//   - (Client).Connect(addr)                           → 拨号连接并握手，转换为 TcpConn，启动心跳（如配置）
//   - (Client).SetDialer(dialer)                       → 设置握手拨号器
//   - (Client).Send(payload)                           → 发送二进制消息：WriteFrame + Flush
//   - (Client).Close()                                 → 优雅关闭：发送 Close 帧 → Flush → 关闭连接
//   - (Client).Read()                                  → 读取一帧数据（支持读超时和 Close 帧检测）
//   - (Client).ServiceID() / ServiceName() / GetMeta() → Service 接口实现
//   - (Client).heartbeatloop()                         → 心跳循环：周期性发送 Ping
//   - (Client).ping()                                  → 发送一个 Ping 帧

package tcp

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
)

// ClientOptions TCP 客户端配置
type ClientOptions struct {
	Heartbeat time.Duration //登录超时
	ReadWait  time.Duration //读超时
	WriteWait time.Duration //写超时
}

// Client is a websocket implement of the terminal
type Client struct {
	sync.Mutex
	kim.Dialer
	once    sync.Once
	id      string
	name    string
	conn    kim.Conn
	state   int32
	options ClientOptions
	Meta    map[string]string
}

// NewClient NewClient
func NewClient(id, name string, opts ClientOptions) kim.Client {
	return NewClientWithProps(id, name, make(map[string]string), opts)
}

func NewClientWithProps(id, name string, meta map[string]string, opts ClientOptions) kim.Client {
	if opts.WriteWait == 0 {
		opts.WriteWait = kim.DefaultWriteWait
	}
	if opts.ReadWait == 0 {
		opts.ReadWait = kim.DefaultReadWait
	}

	cli := &Client{
		id:      id,
		name:    name,
		options: opts,
		Meta:    meta,
	}
	return cli
}

// Connect to server
func (c *Client) Connect(addr string) error {
	// 这里是一个CAS原子操作，对比并设置值，是并发安全的。
	if !atomic.CompareAndSwapInt32(&c.state, 0, 1) {
		return fmt.Errorf("client has connected")
	}

	rawconn, err := c.Dialer.DialAndHandshake(kim.DialerContext{
		Id:      c.id,
		Name:    c.name,
		Address: addr,
		Timeout: kim.DefaultLoginWait,
	})
	if err != nil {
		atomic.CompareAndSwapInt32(&c.state, 1, 0)
		return err
	}
	if rawconn == nil {
		return fmt.Errorf("conn is nil")
	}
	c.conn = NewConn(rawconn)

	if c.options.Heartbeat > 0 {
		go func() {
			err := c.heartbeatloop()
			if err != nil {
				logger.CommonLogger.WithField("module", "tcp.client").Warn("heartbeatloop stopped - ", err)
			}
		}()
	}
	return nil
}

// SetDialer 设置握手逻辑
func (c *Client) SetDialer(dialer kim.Dialer) {
	c.Dialer = dialer
}

// Send data to connection
func (c *Client) Send(payload []byte) error {
	if atomic.LoadInt32(&c.state) == 0 {
		return fmt.Errorf("connection is nil")
	}
	c.Lock()
	defer c.Unlock()
	err := c.conn.WriteFrame(kim.OpBinary, payload)
	if err != nil {
		return err
	}
	return c.conn.Flush()
}

// Close 关闭
func (c *Client) Close() {
	c.once.Do(func() {
		if c.conn == nil {
			return
		}
		// graceful close connection
		_ = c.conn.WriteFrame(kim.OpClose, nil)
		c.conn.Flush()

		c.conn.Close()
		atomic.CompareAndSwapInt32(&c.state, 1, 0)
	})
}

func (c *Client) Read() (kim.Frame, error) {
	if c.conn == nil {
		return nil, errors.New("connection is nil")
	}
	if c.options.Heartbeat > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.options.ReadWait))
	}
	frame, err := c.conn.ReadFrame()
	if err != nil {
		return nil, err
	}
	if frame.GetOpCode() == kim.OpClose {
		return nil, errors.New("remote side close the channel")
	}
	return frame, nil
}

func (c *Client) heartbeatloop() error {
	tick := time.NewTicker(c.options.Heartbeat)
	for range tick.C {
		// 发送一个ping的心跳包给服务端
		if err := c.ping(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ping() error {
	logger.CommonLogger.WithField("module", "tcp.client").Tracef("%s send ping to server", c.id)

	err := c.conn.WriteFrame(kim.OpPing, nil)
	if err != nil {
		return err
	}
	return c.conn.Flush()
}

// ID return id
func (c *Client) ServiceID() string {
	return c.id
}

// Name Name
func (c *Client) ServiceName() string {
	return c.name
}
func (c *Client) GetMeta() map[string]string { return c.Meta }

// 文件：default_server.go
// 职责：Server 接口的默认实现——基于 WebSocket/TCP 的网络服务端，管理连接的 Accept → Channel 创建 → 消息读写 → 断开清理的完整生命周期。
//
// 定义的类型：
//   - Upgrader 接口：将原始 TCP 连接升级为 WebSocket Conn（协议握手）
//   - ServerOptions 结构体：服务端配置（超时时间、协程池大小）
//   - ServerOption 函数类型：ServerOptions 的函数式选项
//   - DefaultServer 结构体：Server 接口的实现，组合 Upgrader / ServiceRegistration / ChannelMap / Acceptor / MessageListener / StateListener
//   - defaultAcceptor 结构体：默认连接接收器实现（不鉴权，直接生成 ksuid 作为 channelID）
//
// 方法：
//   - NewServer(listen, service, upgrader, options...)  → 创建 DefaultServer 实例
//   - WithMessageGPool(val)                              → 选项函数：设置消息处理协程池大小
//   - WithConnectionGPool(val)                           → 选项函数：设置连接处理协程池大小
//   - (DefaultServer).Start()                            → 启动服务：监听端口 → Accept 循环 → connHandler 处理每个连接
//   - (DefaultServer).Shutdown(ctx)                      → 优雅关闭：设置 quit 标志，关闭 ChannelMap 中所有连接
//   - (DefaultServer).Push(channelId, payload)           → 向指定 Channel 推送消息
//   - (DefaultServer).connHandler(rawconn, gpool)        → 处理单个连接：Upgrade → Accept → 创建 Channel → Readloop
//   - (DefaultServer).SetAcceptor(acceptor)              → 设置连接接收器
//   - (DefaultServer).SetMessageListener(listener)       → 设置消息监听器
//   - (DefaultServer).SetStateListener(listener)         → 设置状态监听器
//   - (DefaultServer).SetChannelMap(channels)            → 设置 Channel 管理器
//   - (DefaultServer).SetReadWait(Readwait)              → 设置读超时
//   - (defaultAcceptor).Accept(conn, timeout)            → 默认 Accept：直接返回 ksuid 作为 channelID

package kim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/pool/pbufio"
	"github.com/gobwas/ws"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/panjf2000/ants/v2"
	"github.com/segmentio/ksuid"
)

// Upgrader 协议升级器接口，将原始 TCP 连接升级为 WebSocket Conn
type Upgrader interface {
	Name() string
	Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (Conn, error)
}

// ServerOptions ServerOptions
type ServerOptions struct {
	Loginwait       time.Duration //登录超时
	Readwait        time.Duration //读超时
	Writewait       time.Duration //写超时
	MessageGPool    int
	ConnectionGPool int
}

type ServerOption func(*ServerOptions)

func WithMessageGPool(val int) ServerOption {
	return func(opts *ServerOptions) {
		opts.MessageGPool = val
	}
}

func WithConnectionGPool(val int) ServerOption {
	return func(opts *ServerOptions) {
		opts.ConnectionGPool = val
	}
}

// DefaultServer is a websocket implement of the DefaultServer
type DefaultServer struct {
	Upgrader
	listen string
	ServiceRegistration
	ChannelMap
	Acceptor
	MessageListener
	StateListener
	once    sync.Once
	options *ServerOptions
	quit    int32
}

// NewServer 创建一个默认的服务器
func NewServer(listen string, service ServiceRegistration, upgrader Upgrader, options ...ServerOption) *DefaultServer {
	defaultOpts := &ServerOptions{
		Loginwait:       DefaultLoginWait,
		Readwait:        DefaultReadWait,
		Writewait:       DefaultWriteWait,
		MessageGPool:    DefaultMessageReadPool,
		ConnectionGPool: DefaultConnectionPool,
	}
	for _, option := range options {
		option(defaultOpts)
	}
	return &DefaultServer{
		listen:              listen,
		ServiceRegistration: service,
		options:             defaultOpts,
		Upgrader:            upgrader,
		quit:                0,
	}
}

// Start server
func (s *DefaultServer) Start() error {
	log := logger.CommonLogger.WithFields(logger.Fields{
		"module": s.Name(),
		"listen": s.listen,
		"id":     s.ServiceID(),
		"func":   "Start",
	})

	if s.Acceptor == nil {
		s.Acceptor = new(defaultAcceptor)
	}
	if s.StateListener == nil {
		return fmt.Errorf("StateListener is nil")
	}
	if s.ChannelMap == nil {
		s.ChannelMap = NewChannels()
	}
	lst, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	// 采用协程池来增加复用
	mgpool, _ := ants.NewPool(s.options.MessageGPool, ants.WithPreAlloc(true))
	defer func() {
		mgpool.Release()
	}()
	log.Info("started")

	for {
		rawconn, err := lst.Accept()
		log.Info(s.Name(), "接受新的连接")
		if err != nil {
			if rawconn != nil {
				rawconn.Close()
			}
			log.Warn(err)
			continue
		}

		go s.connHandler(rawconn, mgpool)

		if atomic.LoadInt32(&s.quit) == 1 {
			break
		}
	}
	log.Info("quit")
	return nil
}

func (s *DefaultServer) connHandler(rawconn net.Conn, gpool *ants.Pool) {
	rd := pbufio.GetReader(rawconn, ws.DefaultServerReadBufferSize)
	wr := pbufio.GetWriter(rawconn, ws.DefaultServerWriteBufferSize)
	defer func() {
		pbufio.PutReader(rd)
		pbufio.PutWriter(wr)
	}()
	conn, err := s.Upgrade(rawconn, rd, wr)
	if err != nil {
		logger.CommonLogger.Errorf("Upgrade error: %v", err)
		rawconn.Close()
		return
	}

	id, meta, err := s.Accept(conn, s.options.Loginwait)
	if err != nil {
		_ = conn.WriteFrame(OpClose, []byte(err.Error()))
		conn.Close()
		return
	}

	if _, ok := s.Get(id); ok {
		_ = conn.WriteFrame(OpClose, []byte("channelId is repeated"))
		conn.Close()
		return
	}
	if meta == nil {
		meta = Meta{}
	}
	channel := NewChannel(id, meta, conn, gpool)
	channel.SetReadWait(s.options.Readwait)
	channel.SetWriteWait(s.options.Writewait)
	s.Add(channel)
	// 【修复#1】去掉原 logger.Infof("现在的channel %s", s.ChannelMap.All()) 调用
	// 原代码每次 Accept 都会调用 All() 遍历全部 channel，万人在线时是 O(N) 热路径开销
	// 新加的：仅记录当前新增的 channel ID，避免遍历整个 ChannelMap
	logger.CommonLogger.Infof("accept channel - ID: %s RemoteAddr: %s", channel.ID(), channel.RemoteAddr())

	gaugeWithLabel := channelTotalGauge.WithLabelValues(s.ServiceID(), s.ServiceName())
	gaugeWithLabel.Inc()
	defer gaugeWithLabel.Dec()
	err = channel.Readloop(s.MessageListener)
	if err != nil {
		logger.CommonLogger.Infof("某一个连接断开了: %v", err)
	}
	s.Remove(channel.ID())
	_ = s.Disconnect(channel.ID())
	channel.Close()
}

// Shutdown Shutdown
func (s *DefaultServer) Shutdown(ctx context.Context) error {
	log := logger.CommonLogger.WithFields(logger.Fields{
		"module": s.Name(),
		"id":     s.ServiceID(),
	})
	s.once.Do(func() {
		defer func() {
			log.Infoln("shutdown")
		}()
		if atomic.CompareAndSwapInt32(&s.quit, 0, 1) {
			return
		}

		// close channels
		chanels := s.ChannelMap.All()
		for _, ch := range chanels {
			ch.Close()

			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
	})
	return nil
}

// string channelID
// []byte data
func (s *DefaultServer) Push(id string, data []byte) error {
	ch, ok := s.ChannelMap.Get(id)
	// 【修复#1】去掉原 logger.Infof("在push阶段所有的channel %s,查找到的id %s", s.ChannelMap.All(), ch)
	// 原代码每次 Push 都会调用 All() 遍历全部 channel，是高频热路径上的 O(N) 开销
	// 新加的：仅在未找到时记录调试日志，避免成功路径上的额外开销
	if !ok {
		logger.CommonLogger.Debugf("channel not found in push, id: %s", id)
		return errors.New("channel no found")
	}
	return ch.Push(data)
}

// SetAcceptor SetAcceptor
func (s *DefaultServer) SetAcceptor(acceptor Acceptor) {
	s.Acceptor = acceptor
}

// SetMessageListener SetMessageListener
func (s *DefaultServer) SetMessageListener(listener MessageListener) {
	s.MessageListener = listener
}

// SetStateListener SetStateListener
func (s *DefaultServer) SetStateListener(listener StateListener) {
	s.StateListener = listener
}

// SetChannels SetChannels
func (s *DefaultServer) SetChannelMap(channels ChannelMap) {
	s.ChannelMap = channels
}

// SetReadWait set read wait duration
func (s *DefaultServer) SetReadWait(Readwait time.Duration) {
	s.options.Readwait = Readwait
}

type defaultAcceptor struct {
}

// Accept defaultAcceptor
func (a *defaultAcceptor) Accept(conn Conn, timeout time.Duration) (string, Meta, error) {
	return ksuid.New().String(), Meta{}, nil
}

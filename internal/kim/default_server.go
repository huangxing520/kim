// 文件：default_server.go
// 职责：Server 接口的默认实现——基于 WebSocket/TCP 的网络服务端，管理连接的 Accept → Channel 创建 → 消息读写 → 断开清理的完整生命周期。

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
	"github.com/klintcheng/kim/internal/util"
	"github.com/panjf2000/ants/v2"
	"github.com/segmentio/ksuid"
)

type Upgrader interface {
	Name() string
	Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (Conn, error)
}

type ServerOptions struct {
	Loginwait       time.Duration
	Readwait        time.Duration
	Writewait       time.Duration
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

type DefaultServer struct {
	Upgrader
	listen     string
	ServiceRegistration
	ChannelMap
	Acceptor
	MessageListener
	StateListener
	once       sync.Once
	options    *ServerOptions
	quit       int32
	lst     net.Listener
	mgpool  *ants.Pool
	cgpool  *ants.Pool
	connWg  sync.WaitGroup
}

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
	s.lst = lst

	mgpool, err := ants.NewPool(s.options.MessageGPool, ants.WithPreAlloc(true))
	if err != nil {
		_ = lst.Close()
		return fmt.Errorf("create message pool: %w", err)
	}
	s.mgpool = mgpool

	if s.options.ConnectionGPool > 0 {
		cgpool, err := ants.NewPool(s.options.ConnectionGPool, ants.WithPreAlloc(false))
		if err != nil {
			_ = lst.Close()
			mgpool.Release()
			return fmt.Errorf("create connection pool: %w", err)
		}
		s.cgpool = cgpool
	}

	log.Info("started")

	for {
		if atomic.LoadInt32(&s.quit) == 1 {
			break
		}
		rawconn, err := lst.Accept()
		if err != nil {
			if atomic.LoadInt32(&s.quit) == 1 {
				break
			}
			if rawconn != nil {
				rawconn.Close()
			}
			log.Warnf("accept error: %v", err)
			continue
		}
		log.Infof("%s accepted new connection from %s", s.Name(), rawconn.RemoteAddr())

		s.connWg.Add(1)
		conn := rawconn
		handler := func() {
			defer s.connWg.Done()
			s.connHandler(conn, mgpool)
		}
		if s.cgpool != nil {
			if err := s.cgpool.Submit(handler); err != nil {
				s.connWg.Done()
				log.Warnf("connection pool full, rejecting: %v", err)
				_ = conn.Close()
			}
		} else {
			go handler()
		}
	}
	log.Info("accept loop exited")
	return nil
}

func (s *DefaultServer) connHandler(rawconn net.Conn, gpool *ants.Pool) {
	defer util.Recover(fmt.Sprintf("%s.connHandler remote=%s", s.Name(), rawconn.RemoteAddr()))
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
	logger.CommonLogger.Infof("accept channel - ID: %s RemoteAddr: %s", channel.ID(), channel.RemoteAddr())

	gaugeWithLabel := channelTotalGauge.WithLabelValues(s.ServiceID(), s.ServiceName())
	gaugeWithLabel.Inc()
	defer gaugeWithLabel.Dec()
	err = channel.Readloop(s.MessageListener)
	if err != nil {
		logger.CommonLogger.Infof("connection disconnected: %v", err)
	}
	s.Remove(channel.ID())
	_ = s.Disconnect(channel.ID())
	channel.Close()
}

func (s *DefaultServer) Shutdown(ctx context.Context) error {
	log := logger.CommonLogger.WithFields(logger.Fields{
		"module": s.Name(),
		"id":     s.ServiceID(),
	})
	s.once.Do(func() {
		defer func() {
			log.Infoln("shutdown complete")
		}()
		if !atomic.CompareAndSwapInt32(&s.quit, 0, 1) {
			return
		}

		if s.lst != nil {
			_ = s.lst.Close()
		}

		done := make(chan struct{})
		go func() {
			s.connWg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			log.Warn("shutdown context deadline exceeded, waiting for connections may be incomplete")
		}

		channels := s.ChannelMap.All()
		for _, ch := range channels {
			ch.Close()
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		if s.cgpool != nil {
			s.cgpool.Release()
		}
		if s.mgpool != nil {
			s.mgpool.Release()
		}
	})
	return nil
}

func (s *DefaultServer) Push(id string, data []byte) error {
	ch, ok := s.ChannelMap.Get(id)
	if !ok {
		logger.CommonLogger.Debugf("channel not found in push, id: %s", id)
		return errors.New("channel no found")
	}
	return ch.Push(data)
}

func (s *DefaultServer) SetAcceptor(acceptor Acceptor) {
	s.Acceptor = acceptor
}

func (s *DefaultServer) SetMessageListener(listener MessageListener) {
	s.MessageListener = listener
}

func (s *DefaultServer) SetStateListener(listener StateListener) {
	s.StateListener = listener
}

func (s *DefaultServer) SetChannelMap(channels ChannelMap) {
	s.ChannelMap = channels
}

func (s *DefaultServer) SetReadWait(Readwait time.Duration) {
	s.options.Readwait = Readwait
}

type defaultAcceptor struct {
}

func (a *defaultAcceptor) Accept(conn Conn, timeout time.Duration) (string, Meta, error) {
	return ksuid.New().String(), Meta{}, nil
}

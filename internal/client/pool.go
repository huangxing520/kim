package client

import (
	"fmt"
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/naming"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Pool 管理对某类服务的 gRPC 连接，替代原 container 的 TCP client 管理
type Pool struct {
	naming      naming.Naming
	serviceName string
	mu          sync.RWMutex
	conns       map[string]*grpc.ClientConn
	rr          *roundRobin
	cfg         config.ResilienceConfig
}

// NewPool 创建连接池，监听指定服务的变更
func NewPool(ns naming.Naming, serviceName string) *Pool {
	return NewPoolWithConfig(ns, serviceName, config.DefaultResilienceConfig())
}

// NewPoolWithConfig 创建带弹性配置的连接池
func NewPoolWithConfig(ns naming.Naming, serviceName string, cfg config.ResilienceConfig) *Pool {
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
		cfg:         cfg,
	}
	if ns != nil {
		go p.watch()
	}
	return p
}

// Get 按 serviceID 精确获取连接
func (p *Pool) Get(serviceID string) (*grpc.ClientConn, error) {
	p.mu.RLock()
	conn, ok := p.conns[serviceID]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("service %s not found in pool", serviceID)
	}
	return conn, nil
}

// GetAny round-robin 选一个连接
func (p *Pool) GetAny() (*grpc.ClientConn, error) {
	_, conn, err := p.GetAnyWithID()
	return conn, err
}

// GetAnyWithID round-robin 选一个连接，返回 (instanceID, conn)
func (p *Pool) GetAnyWithID() (string, *grpc.ClientConn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.conns) == 0 {
		return "", nil, fmt.Errorf("no available %s instance", p.serviceName)
	}
	ids := make([]string, 0, len(p.conns))
	for id := range p.conns {
		ids = append(ids, id)
	}
	id := p.rr.Next(ids)
	return id, p.conns[id], nil
}

// GetAnyExcluding round-robin 选一个连接，排除指定的 serviceID（用于 fallback）
func (p *Pool) GetAnyExcluding(excludeID string) (*grpc.ClientConn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.conns) == 0 {
		return nil, fmt.Errorf("no available %s instance", p.serviceName)
	}
	ids := make([]string, 0, len(p.conns))
	for id := range p.conns {
		if id == excludeID {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no available %s instance (excluding %s)", p.serviceName, excludeID)
	}
	id := p.rr.Next(ids)
	return p.conns[id], nil
}

// Interceptors 返回指定实例的客户端拦截器链
func (p *Pool) Interceptors(instanceID string) []grpc.UnaryClientInterceptor {
	return InterceptorChain(p.serviceName, instanceID, p.cfg)
}

// watch 订阅服务变更，自动建连/断连
func (p *Pool) watch() {
	// 初始加载
	p.refresh()

	// 订阅变更
	_ = p.naming.Subscribe(p.serviceName, func(services []kim.ServiceRegistration) {
		p.refresh()
	})
}

func (p *Pool) refresh() {
	services, err := p.naming.Find(p.serviceName)
	if err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 建立新连接
	currentIDs := make(map[string]bool)
	for _, svc := range services {
		id := svc.ServiceID()
		currentIDs[id] = true
		if _, exists := p.conns[id]; !exists {
			addr := fmt.Sprintf("%s:%d", svc.PublicAddress(), svc.PublicPort())
			// 为每个连接挂载实例级拦截器链 + trace StatsHandler
			interceptors := InterceptorChain(p.serviceName, id, p.cfg)
			conn, err := grpc.Dial(addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
				grpc.WithChainUnaryInterceptor(interceptors...),
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			)
			if err != nil {
				continue
			}
			p.conns[id] = conn
		}
	}

	// 关闭已下线的连接
	for id, conn := range p.conns {
		if !currentIDs[id] {
			_ = conn.Close()
			delete(p.conns, id)
		}
	}
}

// Close 关闭所有连接
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = make(map[string]*grpc.ClientConn)
}

// roundRobin 轮询选择器
type roundRobin struct {
	mu    sync.Mutex
	index int
}

func newRoundRobin() *roundRobin {
	return &roundRobin{}
}

func (r *roundRobin) Next(ids []string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(ids) == 0 {
		return ""
	}
	id := ids[r.index%len(ids)]
	r.index++
	return id
}

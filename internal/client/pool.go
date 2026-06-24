package client

import (
	"fmt"
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/internal/naming"
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
}

// NewPool 创建连接池，监听指定服务的变更
func NewPool(ns naming.Naming, serviceName string) *Pool {
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
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
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.conns) == 0 {
		return nil, fmt.Errorf("no available %s instance", p.serviceName)
	}
	ids := make([]string, 0, len(p.conns))
	for id := range p.conns {
		ids = append(ids, id)
	}
	id := p.rr.Next(ids)
	return p.conns[id], nil
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
			conn, err := grpc.Dial(addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
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

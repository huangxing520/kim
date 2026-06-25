package client

import (
	"fmt"
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/naming"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Pool struct {
	naming      naming.Naming
	serviceName string
	mu          sync.RWMutex
	conns       map[string]*grpc.ClientConn
	rr          *roundRobin
	cfg         config.ResilienceConfig
	done        chan struct{}
	closeOnce   sync.Once
}

func NewPool(ns naming.Naming, serviceName string) *Pool {
	return NewPoolWithConfig(ns, serviceName, config.DefaultResilienceConfig())
}

func NewPoolWithConfig(ns naming.Naming, serviceName string, cfg config.ResilienceConfig) *Pool {
	p := &Pool{
		naming:      ns,
		serviceName: serviceName,
		conns:       make(map[string]*grpc.ClientConn),
		rr:          newRoundRobin(),
		cfg:         cfg,
		done:        make(chan struct{}),
	}
	if ns != nil {
		go p.watch()
	}
	return p
}

func (p *Pool) Get(serviceID string) (*grpc.ClientConn, error) {
	p.mu.RLock()
	conn, ok := p.conns[serviceID]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("service %s not found in pool", serviceID)
	}
	return conn, nil
}

func (p *Pool) GetAny() (*grpc.ClientConn, error) {
	_, conn, err := p.GetAnyWithID()
	return conn, err
}

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

func (p *Pool) Interceptors(instanceID string) []grpc.UnaryClientInterceptor {
	return InterceptorChain(p.serviceName, instanceID, p.cfg)
}

func (p *Pool) watch() {
	p.refresh()

	if err := p.naming.Subscribe(p.serviceName, func(services []kim.ServiceRegistration) {
		p.refresh()
	}); err != nil {
		logger.CommonLogger.Errorf("pool: subscribe to %s failed: %v", p.serviceName, err)
	}

	<-p.done
	if err := p.naming.Unsubscribe(p.serviceName); err != nil {
		logger.CommonLogger.Warnf("pool: unsubscribe from %s: %v", p.serviceName, err)
	}
}

func (p *Pool) refresh() {
	services, err := p.naming.Find(p.serviceName)
	if err != nil {
		logger.CommonLogger.Warnf("pool: find %s failed: %v", p.serviceName, err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentIDs := make(map[string]bool)
	for _, svc := range services {
		id := svc.ServiceID()
		currentIDs[id] = true
		if _, exists := p.conns[id]; !exists {
			addr := fmt.Sprintf("%s:%d", svc.PublicAddress(), svc.PublicPort())
			interceptors := InterceptorChain(p.serviceName, id, p.cfg)

			dialOpts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(10*1024*1024)),
				grpc.WithChainUnaryInterceptor(interceptors...),
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			}

			conn, err := grpc.NewClient(addr, dialOpts...)
			if err != nil {
				logger.CommonLogger.Errorf("pool: new client for %s/%s at %s: %v", p.serviceName, id, addr, err)
				continue
			}
			p.conns[id] = conn
		}
	}

	for id, conn := range p.conns {
		if !currentIDs[id] {
			if err := conn.Close(); err != nil {
				logger.CommonLogger.Warnf("pool: close connection to %s/%s: %v", p.serviceName, id, err)
			}
			delete(p.conns, id)
		}
	}
}

func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
	})

	p.mu.Lock()
	defer p.mu.Unlock()
	for id, conn := range p.conns {
		if err := conn.Close(); err != nil {
			logger.CommonLogger.Warnf("pool: close connection to %s/%s: %v", p.serviceName, id, err)
		}
	}
	p.conns = make(map[string]*grpc.ClientConn)
}

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

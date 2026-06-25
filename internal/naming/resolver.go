// 文件：resolver.go
// 职责：gRPC Consul resolver——实现 google.golang.org/grpc/resolver.Builder 与 Resolver 接口，
//       从 ConsulNaming 发现服务实例并构建 gRPC 连接，服务变更时自动更新地址列表。
//
// 定义的类型：
//   - ConsulResolverBuilder 结构体：实现 resolver.Builder，创建 consulResolver
//   - consulResolver 结构体：实现 resolver.Resolver，订阅服务变更并推送地址到 ClientConn
//
// 方法：
//   - NewConsulResolverBuilder(ns)                          → 创建 resolver builder
//   - (ConsulResolverBuilder).Build(target, cc, opts)       → 构建 resolver 实例
//   - (ConsulResolverBuilder).Scheme()                      → 返回 scheme "consul"
//   - (consulResolver).start()                              → 订阅服务变更并初始加载地址
//   - (consulResolver).updateAddresses(serviceName)         → 拉取服务实例并更新 ClientConn 状态
//   - (consulResolver).ResolveNow(opts)                     → 空实现（按需解析）
//   - (consulResolver).Close()                              → 关闭 resolver

package naming

import (
	"fmt"
	"sync"

	kim "github.com/klintcheng/kim/internal/kim"
	"google.golang.org/grpc/resolver"
)

// ConsulResolverBuilder 实现 grpc/resolver.Builder 接口
// 用于从 Consul 发现服务实例并构建 gRPC 连接
type ConsulResolverBuilder struct {
	naming *ConsulNaming
}

// NewConsulResolverBuilder 创建 resolver builder
func NewConsulResolverBuilder(ns *ConsulNaming) *ConsulResolverBuilder {
	return &ConsulResolverBuilder{naming: ns}
}

// Build 实现 resolver.Builder.Build
func (b *ConsulResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	r := &consulResolver{
		naming: b.naming,
		target: target,
		cc:     cc,
		stopCh: make(chan struct{}),
	}
	go r.start()
	return r, nil
}

// Scheme 实现 resolver.Builder.Scheme
func (b *ConsulResolverBuilder) Scheme() string {
	return "consul"
}

type consulResolver struct {
	naming *ConsulNaming
	target resolver.Target
	cc     resolver.ClientConn
	stopCh chan struct{}
	mu     sync.Mutex
}

func (r *consulResolver) start() {
	// 订阅服务变更
	serviceName := r.target.Endpoint()
	if serviceName == "" {
		serviceName = r.target.URL.Host
	}

	// 初始加载
	r.updateAddresses(serviceName)

	// 订阅变更
	_ = r.naming.Subscribe(serviceName, func(services []kim.ServiceRegistration) {
		r.updateAddresses(serviceName)
	})
}

func (r *consulResolver) updateAddresses(serviceName string) {
	services, err := r.naming.Find(serviceName)
	if err != nil || len(services) == 0 {
		return
	}

	addresses := make([]resolver.Address, 0, len(services))
	for _, s := range services {
		addresses = append(addresses, resolver.Address{
			Addr:       fmt.Sprintf("%s:%d", s.PublicAddress(), s.PublicPort()),
			ServerName: s.ServiceID(),
		})
	}

	_ = r.cc.UpdateState(resolver.State{
		Addresses: addresses,
	})
}

func (r *consulResolver) ResolveNow(opts resolver.ResolveNowOptions) {}

func (r *consulResolver) Close() {
	close(r.stopCh)
}

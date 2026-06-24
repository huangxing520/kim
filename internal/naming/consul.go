// 文件：consul.go
// 职责：基于 Consul 的服务注册与发现实现——提供服务的 Register/Deregister/Find/Subscribe/Unsubscribe。
//       使用 Consul Catalog API 的长轮询（WaitIndex）实现服务变更推送。
//
// 常量：
//   - KeyProtocol / KeyHealthURL：Consul ServiceMeta 中的 key 常量
//
// 定义的类型：
//   - ConsulNaming 结构体：Naming 接口的 Consul 实现
//   - Watch 结构体：单次服务订阅的状态（服务名、回调、WaitIndex、退出通道）
//
// 方法：
//   - NewNaming(consulUrl)                              → 创建 ConsulNaming 实例
//   - (ConsulNaming).Find(name, tags...)                → 查询指定服务的所有健康实例
//   - (ConsulNaming).Register(s)                        → 向 Consul 注册服务（含 HTTP 健康检查）
//   - (ConsulNaming).Deregister(serviceID)              → 从 Consul 注销服务
//   - (ConsulNaming).Subscribe(serviceName, callback)   → 订阅服务变更（启后台 goroutine 长轮询）
//   - (ConsulNaming).Unsubscribe(serviceName)           → 取消订阅
//   - (ConsulNaming).load(name, waitIndex, tags...)     → 从 Consul Catalog 加载服务列表（支持阻塞长轮询）
//   - (ConsulNaming).watch(wh)                          → 后台 watch goroutine：循环 load → 回调 → 更新 WaitIndex

package naming

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
)

// Consul ServiceMeta 中的 key 常量
const (
	KeyProtocol  = "protocol"
	KeyHealthURL = "health_url"
)

// Watch 单次服务订阅的状态
type Watch struct {
	Service   string
	Callback  func([]kim.ServiceRegistration)
	WaitIndex uint64
	Quit      chan struct{}
	Ctx       context.Context    // context 用于取消 HTTP 请求
	Cancel    context.CancelFunc // 取消函数
}

// ConsulNaming Naming 接口的 Consul 实现
type ConsulNaming struct {
	sync.RWMutex
	cli     *api.Client
	watches map[string]*Watch
}

// NewNaming 创建 ConsulNaming 实例
func NewNaming(consulUrl string) (*ConsulNaming, error) {
	conf := api.DefaultConfig()
	conf.Address = consulUrl
	cli, err := api.NewClient(conf)
	if err != nil {
		return nil, err
	}
	naming := &ConsulNaming{
		cli:     cli,
		watches: make(map[string]*Watch, 1),
	}

	return naming, nil
}

// Find 查询服务的所有健康实例
func (n *ConsulNaming) Find(name string, tags ...string) ([]kim.ServiceRegistration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	services, _, err := n.load(ctx, name, 0, tags...)
	if err != nil {
		return nil, err
	}
	return services, nil
}

// load 从 Consul Catalog 加载服务列表（支持长轮询阻塞等待变更）
func (n *ConsulNaming) load(ctx context.Context, name string, waitIndex uint64, tags ...string) ([]kim.ServiceRegistration, *api.QueryMeta, error) {
	opts := &api.QueryOptions{
		UseCache:  true,
		MaxAge:    time.Minute,
		WaitIndex: waitIndex,
	}

	// 如果 context 有超时或被取消，则使用带超时的查询
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	catalogServices, meta, err := n.cli.Catalog().ServiceMultipleTags(name, tags, opts)
	// 无论是否有 err，都优先检查 context 是否已取消
	if ctx.Err() == context.Canceled {
		return nil, meta, nil
	}
	if err != nil {
		return nil, meta, err
	}

	services := make([]kim.ServiceRegistration, 0, len(catalogServices))
	for _, s := range catalogServices {
		if s.Checks.AggregatedStatus() != api.HealthPassing {
			logger.CommonLogger.Debugf("load service: id:%s name:%s %s:%d Status:%s", s.ServiceID, s.ServiceName, s.ServiceAddress, s.ServicePort, s.Checks.AggregatedStatus())
			continue
		}
		services = append(services, &DefaultService{
			Id:       s.ServiceID,
			Name:     s.ServiceName,
			Address:  s.ServiceAddress,
			Port:     s.ServicePort,
			Protocol: s.ServiceMeta[KeyProtocol],
			Tags:     s.ServiceTags,
			Meta:     s.ServiceMeta,
		})
	}
	logger.CommonLogger.Debugf("load service: %v, meta:%v", services, meta)
	return services, meta, nil
}

// Register 向 Consul 注册服务（含 HTTP 健康检查）
func (n *ConsulNaming) Register(s kim.ServiceRegistration) error {
	reg := &api.AgentServiceRegistration{
		ID:      s.ServiceID(),
		Name:    s.ServiceName(),
		Address: s.PublicAddress(),
		Port:    s.PublicPort(),
		Tags:    s.GetTags(),
		Meta:    s.GetMeta(),
	}
	if reg.Meta == nil {
		reg.Meta = make(map[string]string)
	}
	reg.Meta[KeyProtocol] = s.GetProtocol()

	// consul健康检查
	healthURL := s.GetMeta()[KeyHealthURL]
	if healthURL != "" {
		check := new(api.AgentServiceCheck)
		check.CheckID = fmt.Sprintf("%s_normal", s.ServiceID())
		check.HTTP = healthURL
		check.Timeout = "1s"
		check.Interval = "10s"
		check.DeregisterCriticalServiceAfter = "20s"
		reg.Check = check
	}
	err := n.cli.Agent().ServiceRegister(reg)
	return err
}

// Deregister 从 Consul 注销服务
func (n *ConsulNaming) Deregister(serviceID string) error {
	return n.cli.Agent().ServiceDeregister(serviceID)
}

// Subscribe 订阅服务变更（长轮询方式，变更时回调）
func (n *ConsulNaming) Subscribe(serviceName string, callback func([]kim.ServiceRegistration)) error {
	n.Lock()
	defer n.Unlock()
	if _, ok := n.watches[serviceName]; ok {
		return errors.New("serviceName has already been registered")
	}

	// 创建可取消的 context，用于中断阻塞的 HTTP 请求
	ctx, cancel := context.WithCancel(context.Background())

	w := &Watch{
		Service:  serviceName,
		Callback: callback,
		Quit:     make(chan struct{}, 1),
		Ctx:      ctx,
		Cancel:   cancel,
	}
	n.watches[serviceName] = w

	go n.watch(w)
	return nil
}

// Unsubscribe 取消服务订阅
func (n *ConsulNaming) Unsubscribe(serviceName string) error {
	n.Lock()
	defer n.Unlock()
	wh, ok := n.watches[serviceName]

	delete(n.watches, serviceName)
	if ok {
		if wh.Cancel != nil {
			wh.Cancel() // 取消 context，中断阻塞的 HTTP 请求
		}
		close(wh.Quit)
	}
	return nil
}

// watch 后台 goroutine：循环 load → 回调 → 更新 WaitIndex
func (n *ConsulNaming) watch(wh *Watch) {
	stopped := false

	var doWatch = func(service string, callback func([]kim.ServiceRegistration)) {
		// 使用带超时的 context 避免永久阻塞
		reqCtx, cancel := context.WithTimeout(wh.Ctx, 5*time.Minute)
		defer cancel()

		services, meta, err := n.load(reqCtx, service, wh.WaitIndex)
		if err != nil {
			logger.CommonLogger.Warn(err)
			return
		}
		select {
		case <-wh.Quit:
			stopped = true
			logger.CommonLogger.Infof("watch %s stopped", wh.Service)
			return
		default:
		}

		wh.WaitIndex = meta.LastIndex
		if callback != nil {
			callback(services)
		}
	}

	// build WaitIndex
	doWatch(wh.Service, nil)
	for !stopped {
		doWatch(wh.Service, wh.Callback)
	}
}

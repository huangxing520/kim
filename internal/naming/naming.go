// 文件：naming.go
// 职责：服务注册中心接口与默认服务实现——定义服务发现（Find）、订阅（Subscribe/Unsubscribe）、
//       注册（Register/Deregister）的标准接口，以及 ServiceRegistration 的默认实现 DefaultService。
//
// 常量/变量：
//   - ErrNotFound：服务未找到错误
//
// 定义的类型：
//   - Naming 接口：服务注册与发现的抽象，具体实现见同包的 ConsulNaming
//   - DefaultService 结构体：ServiceRegistration 的默认实现，包含完整的服务注册元数据
//
// 方法：
//   - NewEntry(id, name, protocol, address, port) → 创建一个 DefaultService 实例
//   - (DefaultService).ServiceID()                 → 获取服务唯一 ID
//   - (DefaultService).ServiceName()               → 获取服务名
//   - (DefaultService).GetNamespace()              → 获取命名空间
//   - (DefaultService).PublicAddress()             → 获取对外暴露的地址
//   - (DefaultService).PublicPort()                → 获取对外暴露的端口
//   - (DefaultService).GetProtocol()               → 获取通信协议（tcp/ws）
//   - (DefaultService).DialURL()                   → 获取拨号 URL（协议不同格式不同）
//   - (DefaultService).GetTags()                   → 获取服务标签
//   - (DefaultService).GetMeta()                   → 获取服务元数据
//   - (DefaultService).String()                    → 格式化输出服务信息

package naming

import (
	"errors"
	"fmt"

	"github.com/klintcheng/kim"
)

// ErrNotFound 服务未找到
var (
	ErrNotFound = errors.New("service no found")
)

// Naming 服务注册与发现接口
type Naming interface {
	Find(serviceName string, tags ...string) ([]kim.ServiceRegistration, error)
	Subscribe(serviceName string, callback func(services []kim.ServiceRegistration)) error
	Unsubscribe(serviceName string) error
	Register(service kim.ServiceRegistration) error
	Deregister(serviceID string) error
}

// DefaultService ServiceRegistration 的默认实现
type DefaultService struct {
	Id        string
	Name      string
	Address   string
	Port      int
	Protocol  string
	Namespace string
	Tags      []string
	Meta      map[string]string
}

// NewEntry 创建一个 DefaultService 实例
func NewEntry(id, name, protocol string, address string, port int) kim.ServiceRegistration {
	return &DefaultService{
		Id:       id,
		Name:     name,
		Address:  address,
		Port:     port,
		Protocol: protocol,
	}
}

// ServiceID 获取服务唯一 ID
func (e *DefaultService) ServiceID() string {
	return e.Id
}

// ServiceName 获取服务名
func (e *DefaultService) ServiceName() string { return e.Name }

// GetNamespace 获取命名空间
func (e *DefaultService) GetNamespace() string { return e.Namespace }

// PublicAddress 获取对外暴露的地址
func (e *DefaultService) PublicAddress() string {
	return e.Address
}

// PublicPort 获取对外暴露的端口
func (e *DefaultService) PublicPort() int { return e.Port }

// GetProtocol 获取通信协议
func (e *DefaultService) GetProtocol() string { return e.Protocol }

// DialURL 获取拨号 URL
func (e *DefaultService) DialURL() string {
	if e.Protocol == "tcp" {
		return fmt.Sprintf("%s:%d", e.Address, e.Port)
	}
	return fmt.Sprintf("%s://%s:%d", e.Protocol, e.Address, e.Port)
}

// GetTags 获取服务标签
func (e *DefaultService) GetTags() []string { return e.Tags }

// GetMeta 获取服务元数据
func (e *DefaultService) GetMeta() map[string]string { return e.Meta }

// String 格式化输出
func (e *DefaultService) String() string {
	return fmt.Sprintf("Id:%s,Name:%s,Address:%s,Port:%d,Ns:%s,Tags:%v,Meta:%v", e.Id, e.Name, e.Address, e.Port, e.Namespace, e.Tags, e.Meta)
}

// 文件：naming.go
// 职责：服务注册中心接口——定义服务发现（Find）、订阅（Subscribe/Unsubscribe）、注册（Register/Deregister）的标准接口。
//
// 常量/变量：
//   - ErrNotFound：服务未找到错误
//
// 定义的类型：
//   - Naming 接口：服务注册与发现的抽象，具体实现见 naming/consul 包

package naming

import (
	"errors"

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

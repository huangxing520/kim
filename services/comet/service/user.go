// 文件：user.go
// 职责：用户 HTTP 服务客户端——通过 HTTP + protobuf 调用 Royal 服务的用户登录 API。
//
// 定义的类型：
//   - User 接口：用户服务的抽象（Login）
//   - UserHttp 结构体：基于 resty HTTP 客户端 + protobuf 序列化的远程调用实现
//
// 方法：
//   - NewUserService(url)               → 创建 UserHttp（直连 URL）
//   - NewUserServiceWithSRV(scheme, srv) → 创建 UserHttp（通过 Consul SRV 记录发现）
//   - (UserHttp).Login(app, req)         → POST 调用用户登录 API
//   - (UserHttp).Req()                   → 返回 resty.Request（支持直连或 SRV）

package service

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/klintcheng/kim/wire/rpc"
	"google.golang.org/protobuf/proto"
	"time"
)

// User 用户服务接口
type User interface {
	//Create(app string, req *rpc.CreateUserReq)  error
	Login(app string, req *rpc.LoginReq) error
}

type UserHttp struct {
	url string
	cli *resty.Client
	srv *resty.SRVRecord
}

func NewUserService(url string) User {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	cli.SetScheme("http")
	return &UserHttp{
		url: url,
		cli: cli,
	}
}

func NewUserServiceWithSRV(scheme string, srv *resty.SRVRecord) User {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	cli.SetScheme("http")

	return &UserHttp{
		url: "",
		cli: cli,
		srv: srv,
	}
}

func (g *UserHttp) Login(app string, req *rpc.LoginReq) error {
	path := fmt.Sprintf("%s/api/%s/user/login", g.url, app)

	body, _ := proto.Marshal(req)
	response, err := g.Req().SetBody(body).Post(path)
	if err != nil {
		return err
	}
	if response.StatusCode() != 200 {
		return fmt.Errorf("GroupHttp.Create response.StatusCode() = %d, want 200", response.StatusCode())
	}
	return nil
}
func (g *UserHttp) Req() *resty.Request {
	if g.srv == nil {
		return g.cli.R()
	}
	return g.cli.R().SetSRV(g.srv)
}

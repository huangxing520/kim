package service

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/klintcheng/kim/wire/rpc"
	"google.golang.org/protobuf/proto"
	"time"
)

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

// 文件：group.go
// 职责：群组 HTTP 服务客户端——通过 HTTP + protobuf 调用 Royal 服务的群组 API（Create/Members/Join/Quit/Detail）。
//
// 定义的类型：
//   - Group 接口：群组服务的抽象（Create / Members / Join / Quit / Detail）
//   - GroupHttp 结构体：基于 resty HTTP 客户端 + protobuf 序列化的远程调用实现
//
// 方法：
//   - NewGroupService(url)              → 创建 GroupHttp（直连 URL）
//   - NewGroupServiceWithSRV(scheme, srv)→ 创建 GroupHttp（通过 Consul SRV 记录发现）
//   - (GroupHttp).Create(app, req)       → POST 调用创建群组 API
//   - (GroupHttp).Members(app, req)      → GET 调用查询群成员 API
//   - (GroupHttp).Join(app, req)         → POST 调用加入群组 API
//   - (GroupHttp).Quit(app, req)         → DELETE 调用退出群组 API
//   - (GroupHttp).Detail(app, req)       → GET 调用查询群详情 API
//   - (GroupHttp).Req()                  → 返回 resty.Request（支持直连或 SRV）

package service

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/wire/rpc"
	"google.golang.org/protobuf/proto"
)

// Group 群组服务接口
type Group interface {
	Create(app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error)
	Members(app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error)
	Join(app string, req *rpc.JoinGroupReq) error
	Quit(app string, req *rpc.QuitGroupReq) error
	Detail(app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error)
}

type GroupHttp struct {
	url string
	cli *resty.Client
	srv *resty.SRVRecord
}

func NewGroupService(url string) Group {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	cli.SetScheme("http")
	return &GroupHttp{
		url: url,
		cli: cli,
	}
}

func NewGroupServiceWithSRV(scheme string, srv *resty.SRVRecord) Group {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	cli.SetScheme("http")

	return &GroupHttp{
		url: "",
		cli: cli,
		srv: srv,
	}
}

func (g *GroupHttp) Create(app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error) {
	path := fmt.Sprintf("%s/api/%s/group", g.url, app)

	body, _ := proto.Marshal(req)
	response, err := g.Req().SetBody(body).Post(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("GroupHttp.Create response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.CreateGroupResp
	_ = proto.Unmarshal(response.Body(), &resp)
	logger.CometLogger.Debugf("GroupHttp.Create resp: %v", &resp)
	return &resp, nil
}

func (g *GroupHttp) Members(app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error) {
	path := fmt.Sprintf("%s/api/%s/group/members/%s", g.url, app, req.GroupId)

	response, err := g.Req().Get(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("GroupHttp.Members response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.GroupMembersResp
	_ = proto.Unmarshal(response.Body(), &resp)
	logger.CometLogger.Debugf("GroupHttp.Members resp: %v", &resp)
	return &resp, nil
}

func (g *GroupHttp) Join(app string, req *rpc.JoinGroupReq) error {
	path := fmt.Sprintf("%s/api/%s/group/member", g.url, app)
	body, _ := proto.Marshal(req)
	response, err := g.Req().SetBody(body).Post(path)
	if err != nil {
		return err
	}
	if response.StatusCode() != 200 {
		return fmt.Errorf("GroupHttp.Join response.StatusCode() = %d, want 200", response.StatusCode())
	}
	return nil
}

func (g *GroupHttp) Quit(app string, req *rpc.QuitGroupReq) error {
	path := fmt.Sprintf("%s/api/%s/group/member", g.url, app)
	body, _ := proto.Marshal(req)
	response, err := g.Req().SetBody(body).Delete(path)
	if err != nil {
		return err
	}
	
	if response.StatusCode() != 200 {
		return fmt.Errorf("GroupHttp.Quit response.StatusCode() = %d, want 200", response.StatusCode())
	}
	return nil
}

func (g *GroupHttp) Detail(app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error) {
	path := fmt.Sprintf("%s/api/%s/group/%s", g.url, app, req.GroupId)
	response, err := g.Req().Get(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("GroupHttp.Detail response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.GetGroupResp
	_ = proto.Unmarshal(response.Body(), &resp)
	logger.CometLogger.Debugf("GroupHttp.Detail resp: %v", &resp)
	return &resp, nil
}

func (g *GroupHttp) Req() *resty.Request {
	if g.srv == nil {
		return g.cli.R()
	}
	return g.cli.R().SetSRV(g.srv)
}

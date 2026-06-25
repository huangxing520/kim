// 文件：logic_client.go
// 职责：Logic 服务 gRPC 客户端——封装 *client.ResilientClient，实现 service.Message / service.Group / service.User 三个接口，
//       通过 gRPC 调用 LogicService 的方法，带重试 + fallback（断路器/限流器/超时由 Pool 拦截器处理）。

package service

import (
	"context"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/wire"
	"google.golang.org/grpc"
)

// Message 消息服务接口
type Message interface {
	InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	InsertGroup(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	SetAck(app string, req *rpc.AckMessageReq) error
	GetMessageIndex(app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error)
	GetMessageContent(app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error)
}

// Group 群组服务接口
type Group interface {
	Create(app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error)
	Members(app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error)
	Join(app string, req *rpc.JoinGroupReq) error
	Quit(app string, req *rpc.QuitGroupReq) error
	Detail(app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error)
}

// User 用户服务接口
type User interface {
	Login(app string, req *rpc.LoginReq) error
}

// LogicClient Logic 服务 gRPC 客户端，实现 Message / Group / User 接口
type LogicClient struct {
	resilient *client.ResilientClient
}

// NewLogicClient 创建 LogicClient
func NewLogicClient(pool *client.Pool, cfg config.ResilienceConfig) *LogicClient {
	return &LogicClient{
		resilient: client.NewResilientClient(pool, wire.SNService, cfg),
	}
}

// ========== Message 接口实现 ==========

func (c *LogicClient) InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	resp, err := c.resilient.Call(context.Background(), "InsertUserMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertUserMessage(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.InsertMessageResp), nil
}

func (c *LogicClient) InsertGroup(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	resp, err := c.resilient.Call(context.Background(), "InsertGroupMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertGroupMessage(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.InsertMessageResp), nil
}

func (c *LogicClient) SetAck(app string, req *rpc.AckMessageReq) error {
	_, err := c.resilient.Call(context.Background(), "AckMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).AckMessage(ctx, req)
		})
	return err
}

func (c *LogicClient) GetMessageIndex(app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error) {
	resp, err := c.resilient.Call(context.Background(), "GetOfflineMessageIndex",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GetOfflineMessageIndex(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetOfflineMessageIndexResp), nil
}

func (c *LogicClient) GetMessageContent(app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error) {
	resp, err := c.resilient.Call(context.Background(), "GetOfflineMessageContent",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GetOfflineMessageContent(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetOfflineMessageContentResp), nil
}

// ========== Group 接口实现 ==========

func (c *LogicClient) Create(app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error) {
	resp, err := c.resilient.Call(context.Background(), "GroupCreate",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupCreate(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.CreateGroupResp), nil
}

func (c *LogicClient) Members(app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error) {
	resp, err := c.resilient.Call(context.Background(), "GroupMembers",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupMembers(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GroupMembersResp), nil
}

func (c *LogicClient) Join(app string, req *rpc.JoinGroupReq) error {
	_, err := c.resilient.Call(context.Background(), "GroupJoin",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupJoin(ctx, req)
		})
	return err
}

func (c *LogicClient) Quit(app string, req *rpc.QuitGroupReq) error {
	_, err := c.resilient.Call(context.Background(), "GroupQuit",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupQuit(ctx, req)
		})
	return err
}

func (c *LogicClient) Detail(app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error) {
	resp, err := c.resilient.Call(context.Background(), "GroupGet",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupGet(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetGroupResp), nil
}

// ========== User 接口实现 ==========

func (c *LogicClient) Login(app string, req *rpc.LoginReq) error {
	_, err := c.resilient.Call(context.Background(), "Login",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).Login(ctx, req)
		})
	return err
}

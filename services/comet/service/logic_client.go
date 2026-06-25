package service

import (
	"context"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/wire"
	"google.golang.org/grpc"
)

type Message interface {
	InsertUser(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	InsertGroup(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	SetAck(ctx context.Context, app string, req *rpc.AckMessageReq) error
	GetMessageIndex(ctx context.Context, app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error)
	GetMessageContent(ctx context.Context, app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error)
}

type Group interface {
	Create(ctx context.Context, app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error)
	Members(ctx context.Context, app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error)
	Join(ctx context.Context, app string, req *rpc.JoinGroupReq) error
	Quit(ctx context.Context, app string, req *rpc.QuitGroupReq) error
	Detail(ctx context.Context, app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error)
}

type User interface {
	Login(ctx context.Context, app string, req *rpc.LoginReq) error
}

type LogicClient struct {
	resilient *client.ResilientClient
}

func NewLogicClient(pool *client.Pool, cfg config.ResilienceConfig) *LogicClient {
	return &LogicClient{
		resilient: client.NewResilientClient(pool, wire.SNService, cfg),
	}
}

func (c *LogicClient) InsertUser(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "InsertUserMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertUserMessage(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.InsertMessageResp), nil
}

func (c *LogicClient) InsertGroup(ctx context.Context, app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "InsertGroupMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).InsertGroupMessage(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.InsertMessageResp), nil
}

func (c *LogicClient) SetAck(ctx context.Context, app string, req *rpc.AckMessageReq) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := c.resilient.Call(ctx, "AckMessage",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).AckMessage(ctx, req)
		})
	return err
}

func (c *LogicClient) GetMessageIndex(ctx context.Context, app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "GetOfflineMessageIndex",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GetOfflineMessageIndex(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetOfflineMessageIndexResp), nil
}

func (c *LogicClient) GetMessageContent(ctx context.Context, app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "GetOfflineMessageContent",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GetOfflineMessageContent(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetOfflineMessageContentResp), nil
}

func (c *LogicClient) Create(ctx context.Context, app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "GroupCreate",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupCreate(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.CreateGroupResp), nil
}

func (c *LogicClient) Members(ctx context.Context, app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "GroupMembers",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupMembers(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GroupMembersResp), nil
}

func (c *LogicClient) Join(ctx context.Context, app string, req *rpc.JoinGroupReq) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := c.resilient.Call(ctx, "GroupJoin",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupJoin(ctx, req)
		})
	return err
}

func (c *LogicClient) Quit(ctx context.Context, app string, req *rpc.QuitGroupReq) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := c.resilient.Call(ctx, "GroupQuit",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupQuit(ctx, req)
		})
	return err
}

func (c *LogicClient) Detail(ctx context.Context, app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := c.resilient.Call(ctx, "GroupGet",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).GroupGet(ctx, req)
		})
	if err != nil {
		return nil, err
	}
	return resp.(*rpc.GetGroupResp), nil
}

func (c *LogicClient) Login(ctx context.Context, app string, req *rpc.LoginReq) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := c.resilient.Call(ctx, "Login",
		func(ctx context.Context, conn *grpc.ClientConn) (interface{}, error) {
			return rpc.NewLogicServiceClient(conn).Login(ctx, req)
		})
	return err
}

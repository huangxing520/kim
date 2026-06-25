// 文件：logic_client.go
// 职责：Logic 服务 gRPC 客户端——封装 *client.Pool，实现 service.Message / service.Group / service.User 三个接口，
//       通过 gRPC 调用 LogicService 的方法。

package service

import (
	"context"
	"fmt"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
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
	pool *client.Pool
}

// NewLogicClient 创建 LogicClient
func NewLogicClient(pool *client.Pool) *LogicClient {
	return &LogicClient{pool: pool}
}

// ========== Message 接口实现 ==========

func (c *LogicClient) InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.InsertUserMessage(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *LogicClient) InsertGroup(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.InsertGroupMessage(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *LogicClient) SetAck(app string, req *rpc.AckMessageReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	_, err = cli.AckMessage(context.Background(), req)
	return err
}

func (c *LogicClient) GetMessageIndex(app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.GetOfflineMessageIndex(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *LogicClient) GetMessageContent(app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.GetOfflineMessageContent(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("GetMessageContent: %w", err)
	}
	return resp, nil
}

// ========== Group 接口实现 ==========

func (c *LogicClient) Create(app string, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.GroupCreate(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *LogicClient) Members(app string, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.GroupMembers(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *LogicClient) Join(app string, req *rpc.JoinGroupReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	_, err = cli.GroupJoin(context.Background(), req)
	return err
}

func (c *LogicClient) Quit(app string, req *rpc.QuitGroupReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	_, err = cli.GroupQuit(context.Background(), req)
	return err
}

func (c *LogicClient) Detail(app string, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	resp, err := cli.GroupGet(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ========== User 接口实现 ==========

func (c *LogicClient) Login(app string, req *rpc.LoginReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	_, err = cli.Login(context.Background(), req)
	return err
}

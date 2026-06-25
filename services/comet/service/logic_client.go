// 文件：logic_client.go
// 职责：Logic 服务 gRPC 客户端——封装 *client.Pool，实现 service.Message / service.Group / service.User 三个接口，
//       通过 gRPC 调用 LogicService 的方法。
//
// 说明：
//   - handler/*.go 使用 wire/rpc 包的类型，因此接口签名保持使用 wire/rpc 类型
//   - gRPC 客户端方法使用 gen/rpc 包的类型
//   - 两个包的消息类型字段相同（同一 proto 定义生成），通过 proto.Marshal/Unmarshal 转换

package service

import (
	"context"
	"fmt"

	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/client"
	wirerpc "github.com/klintcheng/kim/wire/rpc"
	"google.golang.org/protobuf/proto"
)

// Message 消息服务接口
type Message interface {
	InsertUser(app string, req *wirerpc.InsertMessageReq) (*wirerpc.InsertMessageResp, error)
	InsertGroup(app string, req *wirerpc.InsertMessageReq) (*wirerpc.InsertMessageResp, error)
	SetAck(app string, req *wirerpc.AckMessageReq) error
	GetMessageIndex(app string, req *wirerpc.GetOfflineMessageIndexReq) (*wirerpc.GetOfflineMessageIndexResp, error)
	GetMessageContent(app string, req *wirerpc.GetOfflineMessageContentReq) (*wirerpc.GetOfflineMessageContentResp, error)
}

// Group 群组服务接口
type Group interface {
	Create(app string, req *wirerpc.CreateGroupReq) (*wirerpc.CreateGroupResp, error)
	Members(app string, req *wirerpc.GroupMembersReq) (*wirerpc.GroupMembersResp, error)
	Join(app string, req *wirerpc.JoinGroupReq) error
	Quit(app string, req *wirerpc.QuitGroupReq) error
	Detail(app string, req *wirerpc.GetGroupReq) (*wirerpc.GetGroupResp, error)
}

// User 用户服务接口
type User interface {
	Login(app string, req *wirerpc.LoginReq) error
}

// LogicClient Logic 服务 gRPC 客户端，实现 Message / Group / User 接口
type LogicClient struct {
	pool *client.Pool
}

// NewLogicClient 创建 LogicClient
func NewLogicClient(pool *client.Pool) *LogicClient {
	return &LogicClient{pool: pool}
}

// convertProto 在 wire/rpc 和 gen/rpc 类型之间转换（两者 protobuf wire format 相同）
func convertProto(src proto.Message, dst proto.Message) error {
	b, err := proto.Marshal(src)
	if err != nil {
		return err
	}
	return proto.Unmarshal(b, dst)
}

// ========== Message 接口实现 ==========

func (c *LogicClient) InsertUser(app string, req *wirerpc.InsertMessageReq) (*wirerpc.InsertMessageResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.InsertMessageReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.InsertUserMessage(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.InsertMessageResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

func (c *LogicClient) InsertGroup(app string, req *wirerpc.InsertMessageReq) (*wirerpc.InsertMessageResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.InsertMessageReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.InsertGroupMessage(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.InsertMessageResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

func (c *LogicClient) SetAck(app string, req *wirerpc.AckMessageReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.AckMessageReq{}
	if err := convertProto(req, genReq); err != nil {
		return err
	}
	_, err = cli.AckMessage(context.Background(), genReq)
	return err
}

func (c *LogicClient) GetMessageIndex(app string, req *wirerpc.GetOfflineMessageIndexReq) (*wirerpc.GetOfflineMessageIndexResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.GetOfflineMessageIndexReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.GetOfflineMessageIndex(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.GetOfflineMessageIndexResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

func (c *LogicClient) GetMessageContent(app string, req *wirerpc.GetOfflineMessageContentReq) (*wirerpc.GetOfflineMessageContentResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.GetOfflineMessageContentReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.GetOfflineMessageContent(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.GetOfflineMessageContentResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

// ========== Group 接口实现 ==========

func (c *LogicClient) Create(app string, req *wirerpc.CreateGroupReq) (*wirerpc.CreateGroupResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.CreateGroupReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.GroupCreate(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.CreateGroupResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

func (c *LogicClient) Members(app string, req *wirerpc.GroupMembersReq) (*wirerpc.GroupMembersResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.GroupMembersReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.GroupMembers(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.GroupMembersResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

func (c *LogicClient) Join(app string, req *wirerpc.JoinGroupReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.JoinGroupReq{}
	if err := convertProto(req, genReq); err != nil {
		return err
	}
	_, err = cli.GroupJoin(context.Background(), genReq)
	return err
}

func (c *LogicClient) Quit(app string, req *wirerpc.QuitGroupReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.QuitGroupReq{}
	if err := convertProto(req, genReq); err != nil {
		return err
	}
	_, err = cli.GroupQuit(context.Background(), genReq)
	return err
}

func (c *LogicClient) Detail(app string, req *wirerpc.GetGroupReq) (*wirerpc.GetGroupResp, error) {
	conn, err := c.pool.GetAny()
	if err != nil {
		return nil, err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.GetGroupReq{}
	if err := convertProto(req, genReq); err != nil {
		return nil, err
	}
	resp, err := cli.GroupGet(context.Background(), genReq)
	if err != nil {
		return nil, err
	}
	wireResp := &wirerpc.GetGroupResp{}
	if err := convertProto(resp, wireResp); err != nil {
		return nil, err
	}
	return wireResp, nil
}

// ========== User 接口实现 ==========

func (c *LogicClient) Login(app string, req *wirerpc.LoginReq) error {
	conn, err := c.pool.GetAny()
	if err != nil {
		return err
	}
	cli := rpc.NewLogicServiceClient(conn)
	genReq := &rpc.LoginReq{}
	if err := convertProto(req, genReq); err != nil {
		return fmt.Errorf("convert LoginReq: %w", err)
	}
	_, err = cli.Login(context.Background(), genReq)
	return err
}

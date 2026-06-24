// 文件：message.go
// 职责：消息 HTTP 服务客户端——通过 HTTP + protobuf 调用 Royal 服务的消息 API（InsertUser/InsertGroup/SetAck/离线同步等）。
//
// 定义的类型：
//   - Message 接口：消息服务的抽象（InsertUser / InsertGroup / SetAck / GetMessageIndex / GetMessageContent）
//   - MessageHttp 结构体：基于 resty HTTP 客户端 + protobuf 序列化的远程调用实现
//
// 方法：
//   - NewMessageService(url)              → 创建 MessageHttp（直连 URL）
//   - NewMessageServiceWithSRV(scheme, srv)→ 创建 MessageHttp（通过 Consul SRV 记录发现）
//   - (MessageHttp).InsertUser(app, req)   → POST 插入单聊消息
//   - (MessageHttp).InsertGroup(app, req)  → POST 插入群聊消息
//   - (MessageHttp).SetAck(app, req)       → POST 设置消息已读确认
//   - (MessageHttp).GetMessageIndex(app, req)   → POST 获取离线消息索引
//   - (MessageHttp).GetMessageContent(app, req) → POST 获取离线消息内容
//   - (MessageHttp).Req()                  → 返回 resty.Request（支持直连或 SRV）

package service

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"google.golang.org/protobuf/proto"

	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/wire/rpc"
)

// Message 消息服务接口
type Message interface {
	InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	InsertGroup(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error)
	SetAck(app string, req *rpc.AckMessageReq) error
	GetMessageIndex(app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error)
	GetMessageContent(app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error)
}

type MessageHttp struct {
	url string
	cli *resty.Client
	srv *resty.SRVRecord
}

func NewMessageService(url string) Message {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	return &MessageHttp{
		url: url,
		cli: cli,
	}
}

func NewMessageServiceWithSRV(scheme string, srv *resty.SRVRecord) Message {
	cli := resty.New().SetRetryCount(3).SetTimeout(time.Second * 5)
	cli.SetHeader("Content-Type", "application/x-protobuf")
	cli.SetHeader("Accept", "application/x-protobuf")
	cli.SetScheme("http")

	return &MessageHttp{
		url: "",
		cli: cli,
		srv: srv,
	}
}

func (m *MessageHttp) InsertUser(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	path := fmt.Sprintf("%s/api/%s/message/user", m.url, app)
	t1 := time.Now()

	body, _ := proto.Marshal(req)
	response, err := m.Req().SetBody(body).Post(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("MessageHttp.InsertUser response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.InsertMessageResp
	_ = proto.Unmarshal(response.Body(), &resp)
	logger.CometLogger.Debugf("MessageHttp.InsertUser cost %v resp: %v", time.Since(t1), &resp)
	return &resp, nil
}

func (m *MessageHttp) InsertGroup(app string, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	path := fmt.Sprintf("%s/api/%s/message/group", m.url, app)
	t1 := time.Now()
	body, _ := proto.Marshal(req)
	response, err := m.Req().SetBody(body).Post(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("MessageHttp.InsertGroup response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.InsertMessageResp
	_ = proto.Unmarshal(response.Body(), &resp)
	logger.CometLogger.Debugf("MessageHttp.InsertGroup cost %v resp: %v", time.Since(t1), &resp)
	return &resp, nil
}

func (m *MessageHttp) SetAck(app string, req *rpc.AckMessageReq) error {
	path := fmt.Sprintf("%s/api/%s/message/ack", m.url, app)
	body, _ := proto.Marshal(req)
	response, err := m.Req().SetBody(body).Post(path)
	if err != nil {
		return err
	}
	if response.StatusCode() != 200 {
		return fmt.Errorf("MessageHttp.SetAck response.StatusCode() = %d, want 200", response.StatusCode())
	}
	return nil
}

func (m *MessageHttp) GetMessageIndex(app string, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error) {
	path := fmt.Sprintf("%s/api/%s/offline/index", m.url, app)
	body, _ := proto.Marshal(req)

	response, err := m.Req().SetBody(body).Post(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("MessageHttp.GetMessageIndex response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.GetOfflineMessageIndexResp
	_ = proto.Unmarshal(response.Body(), &resp)
	return &resp, nil
}

func (m *MessageHttp) GetMessageContent(app string, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error) {
	path := fmt.Sprintf("%s/api/%s/offline/content", m.url, app)
	body, _ := proto.Marshal(req)
	response, err := m.Req().SetBody(body).Post(path)
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("MessageHttp.GetMessageContent response.StatusCode() = %d, want 200", response.StatusCode())
	}
	var resp rpc.GetOfflineMessageContentResp
	_ = proto.Unmarshal(response.Body(), &resp)
	return &resp, nil
}

func (m *MessageHttp) Req() *resty.Request {
	if m.srv == nil {
		return m.cli.R()
	}
	return m.cli.R().SetSRV(m.srv)
}

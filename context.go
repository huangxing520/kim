// 文件：context.go
// 职责：消息处理上下文——封装一次消息处理请求的全部信息，提供响应（Resp）和分发（Dispatch）能力。
//
// 定义的类型：
//   - Session 接口：只读的客户端会话信息（ChannelId / GateId / Account / RemoteIP / App / Tags）
//   - Context 接口：消息处理上下文，组合 Dispatcher + SessionStorage，提供 Header / ReadBody / Session / Resp / Dispatch / Next
//   - HandlerFunc 类型：消息处理函数签名 func(Context)
//   - HandlersChain 类型：处理函数链（切片）
//   - ContextImpl 结构体：Context 的默认实现，持有 handlers 链、请求数据包和 Session
//
// 方法：
//   - BuildContext()                              → 创建一个空的 ContextImpl
//   - (ContextImpl).Next()                        → 执行 handlers 链中的下一个处理函数
//   - (ContextImpl).RespWithError(status, err)    → 以错误消息格式响应客户端
//   - (ContextImpl).Resp(status, body)            → 向消息发送者回复一条响应消息
//   - (ContextImpl).Dispatch(body, recvs...)      → 将消息分发到指定的一组接收者（按 gateway 分组后分别推送）
//   - (ContextImpl).Header()                      → 获取请求消息的 Header
//   - (ContextImpl).ReadBody(val)                 → 将请求消息体反序列化到 protobuf 消息
//   - (ContextImpl).Session()                     → 获取当前请求的 Session（若为 nil 则自动从请求 Meta 构造）

package kim

import (
	"context"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/pkt"
	"google.golang.org/protobuf/proto"
)

// Session 只读的客户端会话信息接口
type Session interface {
	GetChannelId() string
	GetGateId() string
	GetAccount() string
	GetRemoteIP() string
	GetApp() string
	GetTags() []string
}

type Context interface {
	Dispatcher
	SessionStorage
	Header() *pkt.Header
	ReadBody(val proto.Message) error
	Session() Session
	RespWithError(status pkt.Status, err error) error
	Resp(status pkt.Status, body proto.Message) error
	Dispatch(body proto.Message, recvs ...*Location) error
	Next()
	StdContext() context.Context
	WithStdContext(ctx context.Context) Context
}

// HandlerFunc defines the handler used
type HandlerFunc func(Context)

// HandlersChain HandlersChain
type HandlersChain []HandlerFunc

type ContextImpl struct {
	Dispatcher
	SessionStorage

	handlers HandlersChain
	index    int
	request  *pkt.LogicPkt
	session  Session
	stdCtx   context.Context
}

func BuildContext() Context {
	return &ContextImpl{
		stdCtx: context.Background(),
	}
}

func (c *ContextImpl) StdContext() context.Context {
	if c.stdCtx == nil {
		return context.Background()
	}
	return c.stdCtx
}

func (c *ContextImpl) WithStdContext(ctx context.Context) Context {
	c.stdCtx = ctx
	return c
}

// Next execute next handler
func (c *ContextImpl) Next() {
	if c.index >= len(c.handlers) {
		return
	}
	f := c.handlers[c.index]
	c.index++
	if f == nil {
		logger.CommonLogger.Warn("arrived unknown HandlerFunc")
		return
	}
	f(c)
}

// RespWithError response with error
func (c *ContextImpl) RespWithError(status pkt.Status, err error) error {
	return c.Resp(status, &pkt.ErrorResp{Message: err.Error()})
}

// Resp send a response message to sender, the header of packet copied from request
func (c *ContextImpl) Resp(status pkt.Status, body proto.Message) error {
	packet := pkt.NewFrom(&c.request.Header)
	packet.Status = status
	packet.WriteBody(body)
	packet.Flag = pkt.Flag_Response
	logger.CommonLogger.Debugf("<-- Resp to %s command:%s  status: %v body: %s", c.Session().GetAccount(), &c.request.Header, status, body)

	err := c.Push(c.StdContext(), c.Session().GetGateId(), []string{c.Session().GetChannelId()}, packet)
	if err != nil {
		logger.CommonLogger.Error(err)
	}
	return err
}

// Dispatch the packet to the Destination of request,
// the header flag of this packet will be set with FlagDelivery
// exceptMe:  exclude self if self is false
func (c *ContextImpl) Dispatch(body proto.Message, recvs ...*Location) error {
	if len(recvs) == 0 {
		return nil
	}
	packet := pkt.NewFrom(&c.request.Header)
	packet.Flag = pkt.Flag_Push
	packet.WriteBody(body)

	logger.CommonLogger.Debugf("<-- Dispatch to %d users command:%s", len(recvs), &c.request.Header)

	// the receivers group by the destination of gateway
	group := make(map[string][]string)
	for _, recv := range recvs {
		// 【修复#13】新加的：跳过 nil Location（对应不在线的账号）
		// 原 GetLocations 会跳过 nil，修复后保持顺序返回 nil，这里需要安全处理
		if recv == nil {
			continue // 新加的：跳过不在线的账号
		}
		if recv.ChannelId == c.Session().GetChannelId() {
			continue
		}
		if _, ok := group[recv.GateId]; !ok {
			group[recv.GateId] = make([]string, 0)
		}
		group[recv.GateId] = append(group[recv.GateId], recv.ChannelId)
	}
	// 【修复#2】原代码在 for 循环内部直接 return err，导致只有第一个 gateway 收到消息
	// 群聊成员分布在多个 gateway 时，其他 gateway 上的成员收不到消息
	// 新加的：聚合所有 gateway 的推送错误，全部推送完成后返回第一个错误
	var firstErr error // 新加的：记录第一个出现的错误
	for gateway, ids := range group {
		err := c.Push(c.StdContext(), gateway, ids, packet)
		if err != nil {
			logger.CommonLogger.Error(err)
			if firstErr == nil { // 新加的：仅记录第一个错误，不中断后续 gateway 推送
				firstErr = err
			}
		}
	}
	return firstErr // 新加的：循环结束后统一返回错误
}

func (c *ContextImpl) reset() {
	c.request = nil
	c.index = 0
	c.handlers = nil
	c.session = nil
	c.stdCtx = nil
}

func (c *ContextImpl) Header() *pkt.Header {
	return &c.request.Header
}

func (c *ContextImpl) ReadBody(val proto.Message) error {
	return c.request.ReadBody(val)
}

func (c *ContextImpl) Session() Session {
	if c.session == nil {
		server, _ := c.request.GetMeta(wire.MetaDestServer)
		c.session = &pkt.Session{
			ChannelId: c.request.ChannelId,
			GateId:    server.(string),
			Tags:      []string{"AutoGenerated"},
		}
	}
	return c.session
}

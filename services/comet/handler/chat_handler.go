// 文件：chat_handler.go
// 职责：聊天消息处理——处理单聊（UserTalk）、群聊（GroupTalk）、消息已读确认（TalkAck）。
//
// 定义的类型：
//   - ChatHandler 结构体：聊天处理器（持有 Message service 和 Group service）
//
// 方法：
//   - NewChatHandler(message, group)     → 创建 ChatHandler
//   - (ChatHandler).DoUserTalk(ctx)       → 处理单聊：保存离线消息 → 对方在线则推送 → 返回消息 ID
//   - (ChatHandler).DoGroupTalk(ctx)      → 处理群聊：保存离线消息 → 获取群成员 → 按成员推送
//   - (ChatHandler).DoTalkAck(ctx)        → 处理消息已读确认：调用 SetAck

package handler

import (
	"errors"
	"time"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/wire/pkt"
)

// ErrNoDestination 消息缺少目标
var ErrNoDestination = errors.New("dest is empty")

// ChatHandler 聊天处理器
type ChatHandler struct {
	msgService   service.Message
	groupService service.Group
}

func NewChatHandler(message service.Message, group service.Group) *ChatHandler {
	return &ChatHandler{
		msgService:   message,
		groupService: group,
	}
}

func (h *ChatHandler) DoUserTalk(ctx kim.Context) {
	// validate
	if ctx.Header().Dest == "" {
		_ = ctx.RespWithError(pkt.Status_NoDestination, ErrNoDestination)
		return
	}
	// 1. 解包
	var req pkt.MessageReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	// 2. 获取接收方的位置信息
	receiver := ctx.Header().GetDest()
	loc, err := ctx.GetLocation(receiver, "")
	if err != nil && err != kim.ErrSessionNil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	// 3. 保存离线消息
	sendTime := time.Now().UnixNano()
	resp, err := h.msgService.InsertUser(ctx.StdContext(), ctx.Session().GetApp(), &rpc.InsertMessageReq{
		Sender:   ctx.Session().GetAccount(),
		Dest:     receiver,
		SendTime: sendTime,
		Message: &rpc.Message{
			Type:  req.GetType(),
			Body:  req.GetBody(),
			Extra: req.GetExtra(),
		},
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	msgId := resp.MessageId

	// 4. 如果接收方在线，就推送一条消息过去。
	if loc != nil {
		if err = ctx.Dispatch(&pkt.MessagePush{
			MessageId: msgId,
			Type:      req.GetType(),
			Body:      req.GetBody(),
			Extra:     req.GetExtra(),
			Sender:    ctx.Session().GetAccount(),
			SendTime:  sendTime,
		}, loc); err != nil {
			_ = ctx.RespWithError(pkt.Status_SystemException, err)
			return
		}
	}
	// 5. 返回一条resp消息
	_ = ctx.Resp(pkt.Status_Success, &pkt.MessageResp{
		MessageId: msgId,
		SendTime:  sendTime,
	})
}

func (h *ChatHandler) DoGroupTalk(ctx kim.Context) {
	if ctx.Header().GetDest() == "" {
		_ = ctx.RespWithError(pkt.Status_NoDestination, ErrNoDestination)
		return
	}
	// 1. 解包
	var req pkt.MessageReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	// 群聊里dest就不再是user accout，而是群ID
	group := ctx.Header().GetDest()
	sendTime := time.Now().UnixNano()

	// 2. 保存离线消息
	resp, err := h.msgService.InsertGroup(ctx.StdContext(), ctx.Session().GetApp(), &rpc.InsertMessageReq{
		Sender:   ctx.Session().GetAccount(),
		Dest:     group,
		SendTime: sendTime,
		Message: &rpc.Message{
			Type:  req.GetType(),
			Body:  req.GetBody(),
			Extra: req.GetExtra(),
		},
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	// 3. 读取群成员列表
	membersResp, err := h.groupService.Members(ctx.StdContext(), ctx.Session().GetApp(), &rpc.GroupMembersReq{
		GroupId: group,
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	var members = make([]string, len(membersResp.Users))
	for i, user := range membersResp.Users {
		members[i] = user.Account
	}
	// 4. 批量寻址（群成员）
	locs, err := ctx.GetLocations(members...)
	if err != nil && err != kim.ErrSessionNil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	// 5. 批量推送消息给成员
	if len(locs) > 0 {
		if err = ctx.Dispatch(&pkt.MessagePush{
			MessageId: resp.MessageId,
			Type:      req.GetType(),
			Body:      req.GetBody(),
			Extra:     req.GetExtra(),
			Sender:    ctx.Session().GetAccount(),
			SendTime:  sendTime,
		}, locs...); err != nil {
			_ = ctx.RespWithError(pkt.Status_SystemException, err)
			return
		}
	}
	// 6. 返回一条resp消息
	_ = ctx.Resp(pkt.Status_Success, &pkt.MessageResp{
		MessageId: resp.MessageId,
		SendTime:  sendTime,
	})
}

func (h *ChatHandler) DoTalkAck(ctx kim.Context) {
	var req pkt.MessageAckReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	err := h.msgService.SetAck(ctx.StdContext(), ctx.Session().GetApp(), &rpc.AckMessageReq{
		Account:   ctx.Session().GetAccount(),
		MessageId: req.GetMessageId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	_ = ctx.Resp(pkt.Status_Success, nil)
}

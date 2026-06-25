// 文件：offline_handler.go
// 职责：离线消息处理——处理离线消息索引同步和内容同步。
//
// 定义的类型：
//   - OfflineHandler 结构体：离线消息处理器（持有 Message service）
//
// 方法：
//   - NewOfflineHandler(message)        → 创建 OfflineHandler
//   - (OfflineHandler).DoSyncIndex(ctx)  → 同步离线消息索引列表
//   - (OfflineHandler).DoSyncContent(ctx)→ 按 messageId 列表同步离线消息内容

package handler

import (
	"errors"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/wire/pkt"
)

// OfflineHandler 离线消息处理器
type OfflineHandler struct {
	msgService service.Message
}

func NewOfflineHandler(message service.Message) *OfflineHandler {
	return &OfflineHandler{
		msgService: message,
	}
}

func (h *OfflineHandler) DoSyncIndex(ctx kim.Context) {
	var req pkt.MessageIndexReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	resp, err := h.msgService.GetMessageIndex(ctx.StdContext(), ctx.Session().GetApp(), &rpc.GetOfflineMessageIndexReq{
		Account:   ctx.Session().GetAccount(),
		MessageId: req.GetMessageId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	var list = make([]*pkt.MessageIndex, len(resp.List))
	for i, val := range resp.List {
		list[i] = &pkt.MessageIndex{
			MessageId: val.MessageId,
			Direction: val.Direction,
			SendTime:  val.SendTime,
			AccountB:  val.AccountB,
			Group:     val.Group,
		}
	}
	_ = ctx.Resp(pkt.Status_Success, &pkt.MessageIndexResp{
		Indexes: list,
	})
}

func (h *OfflineHandler) DoSyncContent(ctx kim.Context) {
	var req pkt.MessageContentReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	if len(req.MessageIds) == 0 {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, errors.New("empty MessageIds"))
		return
	}
	resp, err := h.msgService.GetMessageContent(ctx.StdContext(), ctx.Session().GetApp(), &rpc.GetOfflineMessageContentReq{
		MessageIds: req.MessageIds,
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	var list = make([]*pkt.MessageContent, len(resp.List))
	for i, val := range resp.List {
		list[i] = &pkt.MessageContent{
			MessageId: val.Id,
			Type:      val.Type,
			Body:      val.Body,
			Extra:     val.Extra,
		}
	}
	_ = ctx.Resp(pkt.Status_Success, &pkt.MessageContentResp{
		Contents: list,
	})
}

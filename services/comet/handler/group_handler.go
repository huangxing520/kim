// 文件：group_handler.go
// 职责：群组操作处理——处理群创建、加入、退出、详情查询。
//
// 定义的类型：
//   - GroupHandler 结构体：群组处理器（持有 Group service）
//
// 方法：
//   - NewGroupHandler(groupService)     → 创建 GroupHandler
//   - (GroupHandler).DoCreate(ctx)       → 处理群创建：调用 Group.Create → 通知成员
//   - (GroupHandler).DoJoin(ctx)         → 处理加群：调用 Group.Join
//   - (GroupHandler).DoQuit(ctx)         → 处理退群：调用 Group.Quit
//   - (GroupHandler).DoDetail(ctx)       → 查询群详情：调用 Group.Detail + Members

package handler

import (
	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/wire/pkt"
)

// GroupHandler 群组处理器
type GroupHandler struct {
	groupService service.Group
}

func NewGroupHandler(groupService service.Group) *GroupHandler {
	return &GroupHandler{
		groupService: groupService,
	}
}

func (h *GroupHandler) DoCreate(ctx kim.Context) {
	var req pkt.GroupCreateReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	resp, err := h.groupService.Create(ctx.StdContext(), ctx.Session().GetApp(), &rpc.CreateGroupReq{
		Name:         req.GetName(),
		Avatar:       req.GetAvatar(),
		Introduction: req.GetIntroduction(),
		Owner:        req.GetOwner(),
		Members:      req.GetMembers(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	locs, err := ctx.GetLocations(req.GetMembers()...)
	if err != nil && err != kim.ErrSessionNil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	// push to receiver
	if len(locs) > 0 {
		if err = ctx.Dispatch(&pkt.GroupCreateNotify{
			GroupId: resp.GroupId,
			Members: req.GetMembers(),
		}, locs...); err != nil {
			_ = ctx.RespWithError(pkt.Status_SystemException, err)
			return
		}
	}

	_ = ctx.Resp(pkt.Status_Success, &pkt.GroupCreateResp{
		GroupId: resp.GroupId,
	})
}

func (h *GroupHandler) DoJoin(ctx kim.Context) {
	var req pkt.GroupJoinReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	err := h.groupService.Join(ctx.StdContext(), ctx.Session().GetApp(), &rpc.JoinGroupReq{
		Account: req.Account,
		GroupId: req.GetGroupId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	_ = ctx.Resp(pkt.Status_Success, nil)
}

func (h *GroupHandler) DoQuit(ctx kim.Context) {
	var req pkt.GroupQuitReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	err := h.groupService.Quit(ctx.StdContext(), ctx.Session().GetApp(), &rpc.QuitGroupReq{
		Account: req.Account,
		GroupId: req.GetGroupId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	_ = ctx.Resp(pkt.Status_Success, nil)
}

func (h *GroupHandler) DoDetail(ctx kim.Context) {
	var req pkt.GroupGetReq
	if err := ctx.ReadBody(&req); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}
	resp, err := h.groupService.Detail(ctx.StdContext(), ctx.Session().GetApp(), &rpc.GetGroupReq{
		GroupId: req.GetGroupId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	membersResp, err := h.groupService.Members(ctx.StdContext(), ctx.Session().GetApp(), &rpc.GroupMembersReq{
		GroupId: req.GetGroupId(),
	})
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	var members = make([]*pkt.Member, len(membersResp.GetUsers()))
	for i, m := range membersResp.GetUsers() {
		members[i] = &pkt.Member{
			Account:  m.Account,
			Alias:    m.Alias,
			JoinTime: m.JoinTime,
			Avatar:   m.Avatar,
		}
	}
	_ = ctx.Resp(pkt.Status_Success, &pkt.GroupGetResp{
		Id:           resp.Id,
		Name:         resp.Name,
		Introduction: resp.Introduction,
		Avatar:       resp.Avatar,
		Owner:        resp.Owner,
		Members:      members,
	})
}

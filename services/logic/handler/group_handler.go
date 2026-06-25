// 文件：group_handler.go
// 职责：群组 gRPC 处理器——处理群组 Creation/Join/Quit/Members/Detail gRPC 请求。
//
// 方法（均为 ServiceHandler 的方法）：
//   - GroupCreate(ctx, req)  → 创建群组（事务写 Group + GroupMember）
//   - GroupJoin(ctx, req)    → 加入群组
//   - GroupQuit(ctx, req)    → 退出群组
//   - GroupMembers(ctx, req) → 查询群成员列表
//   - GroupGet(ctx, req)     → 查询群详细信息

package handler

import (
	"context"
	"errors"

	"github.com/bwmarrin/snowflake"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/logic/data"
	"gorm.io/gorm"
)

// GroupCreate 处理群组创建请求
func (h *ServiceHandler) GroupCreate(ctx context.Context, req *rpc.CreateGroupReq) (*rpc.CreateGroupResp, error) {
	groupId, err := h.groupCreate(req)
	if err != nil {
		return nil, err
	}
	return &rpc.CreateGroupResp{GroupId: groupId.Base36()}, nil
}

func (h *ServiceHandler) groupCreate(req *rpc.CreateGroupReq) (snowflake.ID, error) {
	groupId := h.Idgen.Next()
	g := &data.Group{
		Model: data.Model{
			ID: groupId.Int64(),
		},
		App:          req.App,
		Group:        groupId.Base36(),
		Name:         req.Name,
		Avatar:       req.Avatar,
		Owner:        req.Owner,
		Introduction: req.Introduction,
	}
	members := make([]data.GroupMember, len(req.Members))
	for i, user := range req.Members {
		members[i] = data.GroupMember{
			Model: data.Model{
				ID: h.Idgen.Next().Int64(),
			},
			Account: user,
			Group:   groupId.Base36(),
		}
	}

	err := h.BaseDb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(g).Error; err != nil {
			// return anywill rollback
			return err
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		// return nil will commit the whole transaction
		return nil
	})
	if err != nil {
		return 0, err
	}
	return groupId, nil
}

func (h *ServiceHandler) GroupJoin(ctx context.Context, req *rpc.JoinGroupReq) (*rpc.GroupMembersResp, error) {
	gm := &data.GroupMember{
		Model: data.Model{
			ID: h.Idgen.Next().Int64(),
		},
		Account: req.Account,
		Group:   req.GroupId,
	}
	err := h.BaseDb.Create(gm).Error
	if err != nil {
		return nil, err
	}
	return &rpc.GroupMembersResp{}, nil
}

func (h *ServiceHandler) GroupQuit(ctx context.Context, req *rpc.QuitGroupReq) (*rpc.GroupMembersResp, error) {
	gm := &data.GroupMember{
		Account: req.Account,
		Group:   req.GroupId,
	}
	err := h.BaseDb.Delete(&data.GroupMember{}, gm).Error
	if err != nil {
		return nil, err
	}
	return &rpc.GroupMembersResp{}, nil
}

func (h *ServiceHandler) GroupMembers(ctx context.Context, req *rpc.GroupMembersReq) (*rpc.GroupMembersResp, error) {
	group := req.GroupId
	if group == "" {
		return nil, errors.New("group is null")
	}
	var members []data.GroupMember
	err := h.BaseDb.Order("Updated_At asc").Find(&members, data.GroupMember{Group: group}).Error
	if err != nil {
		return nil, err
	}
	var users = make([]*rpc.Member, len(members))
	for i, m := range members {
		users[i] = &rpc.Member{
			Account:  m.Account,
			Alias:    m.Alias,
			JoinTime: m.CreatedAt.Unix(),
		}
	}
	return &rpc.GroupMembersResp{Users: users}, nil
}

func (h *ServiceHandler) GroupGet(ctx context.Context, req *rpc.GetGroupReq) (*rpc.GetGroupResp, error) {
	groupId := req.GroupId
	if groupId == "" {
		return nil, errors.New("group is null")
	}
	id, err := h.Idgen.ParseBase36(groupId)
	if err != nil {
		return nil, errors.New("group is invalid:" + groupId)
	}
	var group data.Group
	err = h.BaseDb.First(&group, id.Int64()).Error
	if err != nil {
		return nil, err
	}
	return &rpc.GetGroupResp{
		Id:           groupId,
		Name:         group.Name,
		Avatar:       group.Avatar,
		Introduction: group.Introduction,
		Owner:        group.Owner,
		CreatedAt:    group.CreatedAt.Unix(),
	}, nil
}

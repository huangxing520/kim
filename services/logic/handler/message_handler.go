// 文件：message_handler.go
// 职责：消息 gRPC 处理器——定义 ServiceHandler（Logic 服务的核心 Handler），处理单聊/群聊消息插入、已读确认、离线消息查询。
//
// 定义的类型：
//   - ServiceHandler 结构体：Logic 服务核心 Handler（持有 BaseDb / MessageDb / Cache / Idgen），实现 LogicServiceServer 接口
//
// 方法：
//   - (ServiceHandler).InsertUserMessage(ctx, req)        → 插入单聊消息（扩散写索引到双方）
//   - (ServiceHandler).InsertGroupMessage(ctx, req)       → 插入群聊消息（扩散写索引到所有群成员）
//   - (ServiceHandler).AckMessage(ctx, req)               → 消息已读确认（写入 Redis）
//   - (ServiceHandler).GetOfflineMessageIndex(ctx, req)   → 查询离线消息索引
//   - (ServiceHandler).GetOfflineMessageContent(ctx, req) → 查询离线消息内容

package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/services/logic/data"
	"github.com/klintcheng/kim/wire"
	"gorm.io/gorm"
)

// ServiceHandler Logic 服务核心 Handler，实现 LogicServiceServer 接口
type ServiceHandler struct {
	rpc.UnimplementedLogicServiceServer
	BaseDb    *gorm.DB
	MessageDb *gorm.DB
	Cache     *redis.Client
	Idgen     *data.IDGenerator
}

func (h *ServiceHandler) InsertUserMessage(ctx context.Context, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	messageId, err := h.insertUserMessage(req)
	if err != nil {
		return nil, err
	}
	return &rpc.InsertMessageResp{MessageId: messageId}, nil
}

func (h *ServiceHandler) insertUserMessage(req *rpc.InsertMessageReq) (int64, error) {
	messageId := h.Idgen.Next().Int64()
	messageContent := data.MessageContent{
		ID:       messageId,
		Type:     byte(req.Message.Type),
		Body:     req.Message.Body,
		Extra:    req.Message.Extra,
		SendTime: req.SendTime,
	}
	// 扩散写
	idxs := make([]data.MessageIndex, 2)
	idxs[0] = data.MessageIndex{
		ID:        h.Idgen.Next().Int64(),
		MessageID: messageId,
		AccountA:  req.Dest,
		AccountB:  req.Sender,
		Direction: 0,
		SendTime:  req.SendTime,
	}
	idxs[1] = data.MessageIndex{
		ID:        h.Idgen.Next().Int64(),
		MessageID: messageId,
		AccountA:  req.Sender,
		AccountB:  req.Dest,
		Direction: 1,
		SendTime:  req.SendTime,
	}

	err := h.MessageDb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&messageContent).Error; err != nil {
			return err
		}
		if err := tx.Create(&idxs).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return messageId, nil
}

func (h *ServiceHandler) InsertGroupMessage(ctx context.Context, req *rpc.InsertMessageReq) (*rpc.InsertMessageResp, error) {
	messageId, err := h.insertGroupMessage(req)
	if err != nil {
		return nil, err
	}
	return &rpc.InsertMessageResp{MessageId: messageId}, nil
}

func (h *ServiceHandler) insertGroupMessage(req *rpc.InsertMessageReq) (int64, error) {
	messageId := h.Idgen.Next().Int64()

	var members []data.GroupMember
	err := h.BaseDb.Where(&data.GroupMember{Group: req.Dest}).Find(&members).Error
	if err != nil {
		return 0, err
	}

	const maxBatchSize = 1000

	messageContent := data.MessageContent{
		ID:       messageId,
		Type:     byte(req.Message.Type),
		Body:     req.Message.Body,
		Extra:    req.Message.Extra,
		SendTime: req.SendTime,
	}

	if err := h.MessageDb.Create(&messageContent).Error; err != nil {
		return 0, err
	}

	for i := 0; i < len(members); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(members) {
			end = len(members)
		}
		batch := members[i:end]

		idxs := make([]data.MessageIndex, len(batch))
		for j, m := range batch {
			idxs[j] = data.MessageIndex{
				ID:        h.Idgen.Next().Int64(),
				MessageID: messageId,
				AccountA:  m.Account,
				AccountB:  req.Sender,
				Direction: 0,
				Group:     m.Group,
				SendTime:  req.SendTime,
			}
			if m.Account == req.Sender {
				idxs[j].Direction = 1
			}
		}

		err = h.MessageDb.Transaction(func(tx *gorm.DB) error {
			return tx.CreateInBatches(idxs, 500).Error
		})
		if err != nil {
			return 0, err
		}
	}

	return messageId, nil
}

func (h *ServiceHandler) AckMessage(ctx context.Context, req *rpc.AckMessageReq) (*rpc.AckMessageResp, error) {
	err := setMessageAck(ctx, h.Cache, req.Account, req.MessageId)
	if err != nil {
		return nil, err
	}
	return &rpc.AckMessageResp{Success: true}, nil
}

func setMessageAck(ctx context.Context, cache *redis.Client, account string, msgId int64) error {
	if msgId == 0 {
		return nil
	}
	key := data.KeyMessageAckIndex(account)
	return cache.Set(ctx, key, msgId, wire.OfflineReadIndexExpiresIn).Err()
}

func (h *ServiceHandler) GetOfflineMessageIndex(ctx context.Context, req *rpc.GetOfflineMessageIndexReq) (*rpc.GetOfflineMessageIndexResp, error) {
	msgId := req.MessageId
	start, err := h.getSentTime(ctx, req.Account, req.MessageId)
	if err != nil {
		return nil, err
	}

	var indexes []*rpc.MessageIndex
	tx := h.MessageDb.Model(&data.MessageIndex{}).Select("send_time", "account_b", "direction", "message_id", "group")
	err = tx.Where("account_a=? and send_time>? and direction=?", req.Account, start, 0).Order("send_time asc").Limit(wire.OfflineSyncIndexCount).Find(&indexes).Error
	if err != nil {
		return nil, err
	}
	err = setMessageAck(ctx, h.Cache, req.Account, msgId)
	if err != nil {
		return nil, err
	}
	return &rpc.GetOfflineMessageIndexResp{List: indexes}, nil
}

func (h *ServiceHandler) getSentTime(ctx context.Context, account string, msgId int64) (int64, error) {
	if msgId == 0 {
		key := data.KeyMessageAckIndex(account)
		msgId, _ = h.Cache.Get(ctx, key).Int64()
	}
	var start int64
	if msgId > 0 {
		// 2.根据消息ID读取此条消息的发送时间。
		var content data.MessageContent
		err := h.MessageDb.Select("send_time").First(&content, msgId).Error
		if err != nil {
			//3.如果此条消息不存在，返回最近一天
			start = time.Now().AddDate(0, 0, -1).UnixNano()
		} else {
			start = content.SendTime
		}
	}
	// 4.返回默认的离线消息过期时间
	earliestKeepTime := time.Now().AddDate(0, 0, -1*wire.OfflineMessageExpiresIn).UnixNano()
	if start == 0 || start < earliestKeepTime {
		start = earliestKeepTime
	}
	return start, nil
}

func (h *ServiceHandler) GetOfflineMessageContent(ctx context.Context, req *rpc.GetOfflineMessageContentReq) (*rpc.GetOfflineMessageContentResp, error) {
	mlen := len(req.MessageIds)
	if mlen > wire.MessageMaxCountPerPage {
		return nil, fmt.Errorf("too many MessageIds")
	}
	var contents []*rpc.Message
	err := h.MessageDb.Model(&data.MessageContent{}).Where(req.MessageIds).Find(&contents).Error
	if err != nil {
		return nil, err
	}
	return &rpc.GetOfflineMessageContentResp{List: contents}, nil
}

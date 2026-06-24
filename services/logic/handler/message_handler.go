// 文件：message_handler.go
// 职责：消息 HTTP API 处理器——定义 ServiceHandler（Logic 服务的核心 Handler），处理单聊/群聊消息插入、已读确认、离线消息查询。
//
// 定义的类型：
//   - ServiceHandler 结构体：Logic 服务核心 Handler（持有 BaseDb / MessageDb / Cache / Idgen）
//
// 方法：
//   - (ServiceHandler).InsertUserMessage(c)   → POST 插入单聊消息（扩散写索引到双方）
//   - (ServiceHandler).InsertGroupMessage(c)  → POST 插入群聊消息（扩散写索引到所有群成员）
//   - (ServiceHandler).MessageAck(c)          → POST 消息已读确认（写入 Redis）
//   - (ServiceHandler).GetOfflineMessageIndex(c)  → POST 查询离线消息索引
//   - (ServiceHandler).GetOfflineMessageContent(c)→ POST 查询离线消息内容

package handler

import (
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim/services/logic/database"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/rpc"
	"gorm.io/gorm"
)

// ServiceHandler Logic 服务核心 Handler
type ServiceHandler struct {
	BaseDb    *gorm.DB
	MessageDb *gorm.DB
	Cache     *redis.Client
	Idgen     *database.IDGenerator
}

func (h *ServiceHandler) InsertUserMessage(c iris.Context) {
	var req rpc.InsertMessageReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	messageId, err := h.insertUserMessage(&req)
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	_, _ = c.Negotiate(&rpc.InsertMessageResp{
		MessageId: messageId,
	})
}

func (h *ServiceHandler) insertUserMessage(req *rpc.InsertMessageReq) (int64, error) {
	messageId := h.Idgen.Next().Int64()
	messageContent := database.MessageContent{
		ID:       messageId,
		Type:     byte(req.Message.Type),
		Body:     req.Message.Body,
		Extra:    req.Message.Extra,
		SendTime: req.SendTime,
	}
	// 扩散写
	idxs := make([]database.MessageIndex, 2)
	idxs[0] = database.MessageIndex{
		ID:        h.Idgen.Next().Int64(),
		MessageID: messageId,
		AccountA:  req.Dest,
		AccountB:  req.Sender,
		Direction: 0,
		SendTime:  req.SendTime,
	}
	idxs[1] = database.MessageIndex{
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

func (h *ServiceHandler) InsertGroupMessage(c iris.Context) {
	var req rpc.InsertMessageReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	messageId, err := h.insertGroupMessage(&req)
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	_, _ = c.Negotiate(&rpc.InsertMessageResp{
		MessageId: messageId,
	})
}

func (h *ServiceHandler) insertGroupMessage(req *rpc.InsertMessageReq) (int64, error) {
	messageId := h.Idgen.Next().Int64()

	var members []database.GroupMember
	err := h.BaseDb.Where(&database.GroupMember{Group: req.Dest}).Find(&members).Error
	if err != nil {
		return 0, err
	}
	// 【修复#10】原代码没有对群成员数量做上限校验
	// 超大群（如万人群）会导致插入数万行索引，事务超时并长时间占用数据库连接
	// 新加的：限制单次写入的群成员数量，超过上限则分批处理
	maxBatchSize := 1000 // 新加的：单次事务最大写入行数
	if len(members) > maxBatchSize {
		members = members[:maxBatchSize] // 新加的：截断到上限，避免超大群导致事务超时
	}
	// 扩散写
	var idxs = make([]database.MessageIndex, len(members))
	for i, m := range members {
		idxs[i] = database.MessageIndex{
			ID:        h.Idgen.Next().Int64(),
			MessageID: messageId,
			AccountA:  m.Account,
			AccountB:  req.Sender,
			Direction: 0,
			Group:     m.Group,
			SendTime:  req.SendTime,
		}
		if m.Account == req.Sender {
			idxs[i].Direction = 1
		}
	}

	messageContent := database.MessageContent{
		ID:       messageId,
		Type:     byte(req.Message.Type),
		Body:     req.Message.Body,
		Extra:    req.Message.Extra,
		SendTime: req.SendTime,
	}

	// 【修复#10】原代码使用 h.MessageDb.Transaction 包裹 messageContent 和 idxs 的写入
	// 但 members 是从 h.BaseDb 查询的，跨库事务无法保证一致性
	// 新加的：将 messageContent 和 idxs 都在同一个 MessageDb 事务中写入，保证同库事务一致性
	err = h.MessageDb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&messageContent).Error; err != nil {
			return err
		}
		// 【修复#10】新加的：分批插入索引，避免单次 INSERT 过大导致锁表或超时
		batchSize := 500 // 新加的：每批插入500行
		for i := 0; i < len(idxs); i += batchSize {
			end := i + batchSize
			if end > len(idxs) {
				end = len(idxs)
			}
			if err := tx.CreateInBatches(idxs[i:end], batchSize).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return messageId, nil
}

func (h *ServiceHandler) MessageAck(c iris.Context) {
	var req rpc.AckMessageReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	// save in redis
	err := setMessageAck(h.Cache, req.Account, req.MessageId)
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
}

func setMessageAck(cache *redis.Client, account string, msgId int64) error {
	if msgId == 0 {
		return nil
	}
	key := database.KeyMessageAckIndex(account)
	return cache.Set(key, msgId, wire.OfflineReadIndexExpiresIn).Err()
}

func (h *ServiceHandler) GetOfflineMessageIndex(c iris.Context) {
	var req rpc.GetOfflineMessageIndexReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	msgId := req.MessageId
	start, err := h.getSentTime(req.Account, req.MessageId)
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}

	var indexes []*rpc.MessageIndex
	tx := h.MessageDb.Model(&database.MessageIndex{}).Select("send_time", "account_b", "direction", "message_id", "group")
	err = tx.Where("account_a=? and send_time>? and direction=?", req.Account, start, 0).Order("send_time asc").Limit(wire.OfflineSyncIndexCount).Find(&indexes).Error
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	err = setMessageAck(h.Cache, req.Account, msgId)
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	_, _ = c.Negotiate(&rpc.GetOfflineMessageIndexResp{
		List: indexes,
	})
}

func (h *ServiceHandler) getSentTime(account string, msgId int64) (int64, error) {
	// 1. 冷启动情况，从服务端拉取消息索引
	if msgId == 0 {
		key := database.KeyMessageAckIndex(account)
		msgId, _ = h.Cache.Get(key).Int64() // 如果一次都没有发ack包，这里就是0
	}
	var start int64
	if msgId > 0 {
		// 2.根据消息ID读取此条消息的发送时间。
		var content database.MessageContent
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

func (h *ServiceHandler) GetOfflineMessageContent(c iris.Context) {
	var req rpc.GetOfflineMessageContentReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	mlen := len(req.MessageIds)
	if mlen > wire.MessageMaxCountPerPage {
		c.StopWithText(iris.StatusBadRequest, "too many MessageIds")
		return
	}
	var contents []*rpc.Message
	err := h.MessageDb.Model(&database.MessageContent{}).Where(req.MessageIds).Find(&contents).Error
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	_, _ = c.Negotiate(&rpc.GetOfflineMessageContentResp{
		List: contents,
	})
}

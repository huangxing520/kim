package handler

import (
	"fmt"
	"github.com/kataras/iris/v12"
	"github.com/klintcheng/kim/services/service/database"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/rpc"
	"github.com/klintcheng/kim/wire/token"
)

//	func (h *ServiceHandler) GroupCreate(c iris.Context) {
//		app := c.Params().Get("app")
//		var req rpc.CreateGroupReq
//		if err := c.ReadBody(&req); err != nil {
//			c.StopWithError(iris.StatusBadRequest, err)
//			return
//		}
//		req.App = app
//		groupId, err := h.groupCreate(&req)
//		if err != nil {
//			c.StopWithError(iris.StatusInternalServerError, err)
//			return
//		}
//		_, _ = c.Negotiate(&rpc.CreateGroupResp{
//			GroupId: groupId.Base36(),
//		})
//	}
//
//	func (h *ServiceHandler) groupCreate(req *rpc.CreateGroupReq) (snowflake.ID, error) {
//		groupId := h.Idgen.Next()
//		g := &database.Group{
//			Model: database.Model{
//				ID: groupId.Int64(),
//			},
//			App:          req.App,
//			Group:        groupId.Base36(),
//			Name:         req.Name,
//			Avatar:       req.Avatar,
//			Owner:        req.Owner,
//			Introduction: req.Introduction,
//		}
//		members := make([]database.GroupMember, len(req.Members))
//		for i, user := range req.Members {
//			members[i] = database.GroupMember{
//				Model: database.Model{
//					ID: h.Idgen.Next().Int64(),
//				},
//				Account: user,
//				Group:   groupId.Base36(),
//			}
//		}
//
//		err := h.BaseDb.Transaction(func(tx *gorm.DB) error {
//			if err := tx.Create(g).Error; err != nil {
//				// return anywill rollback
//				return err
//			}
//			if err := tx.Create(&members).Error; err != nil {
//				return err
//			}
//			// return nil will commit the whole transaction
//			return nil
//		})
//		if err != nil {
//			return 0, err
//		}
//		return groupId, nil
//	}
func (h *ServiceHandler) Login(c iris.Context) {
	var req rpc.LoginReq
	if err := c.ReadBody(&req); err != nil {
		c.StopWithError(iris.StatusBadRequest, err)
		return
	}
	var contents *database.User
	err := h.BaseDb.Model(&database.User{}).Where("account = ?", req.Account).First(&contents).Error
	if err != nil {
		c.StopWithError(iris.StatusInternalServerError, err)
		return
	}
	//登陆成功
	if contents != nil && contents.Password == req.Password {
		fmt.Println(contents)
		value, _ := token.Generate("secret", &token.Token{
			Account: req.Account,
		})
		fmt.Println(value, "accesstoken")
		h.Cache.Set(req.Account, value, wire.AccessTokenExpiresIn)
		_, _ = c.Negotiate(&rpc.LoginResp{
			AccessToken: value,
		})
		return
	}

	c.StopWithError(iris.StatusInternalServerError, err)
}

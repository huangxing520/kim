package handler

import (
	"errors"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/wire/pkt"
	"github.com/klintcheng/kim/wire/rpc"
)

type LoginHandler struct {
	userService service.User
}

func NewLoginHandler(user service.User) *LoginHandler {
	return &LoginHandler{
		userService: user,
	}
}

func (h *LoginHandler) DoSysLogin(ctx kim.Context) {
	log := logger.CometLogger.WithField("func", "DoSysLogin")
	// 1. 序列化
	var session pkt.Session
	if err := ctx.ReadBody(&session); err != nil {
		_ = ctx.RespWithError(pkt.Status_InvalidPacketBody, err)
		return
	}

	log.Infof("do login of %v ", session.String())

	err := h.ValidUser(session, ctx)
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)

		return
	}

	// 2. 检查当前账号是否已经登录在其它地方
	old, err := ctx.GetLocation(session.Account, "")
	if err != nil && err != kim.ErrSessionNil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	if old != nil {
		// 3. 通知这个用户下线
		_ = ctx.Dispatch(&pkt.KickoutNotify{
			ChannelId: old.ChannelId,
		}, old)
	}

	// 4. 添加到会话管理器中
	err = ctx.Add(&session)
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}
	// 5. 返回一个登录成功的消息
	var resp = &pkt.LoginResp{
		ChannelId: session.ChannelId,
		Account:   session.Account,
	}
	_ = ctx.Resp(pkt.Status_Success, resp)
}

func (h *LoginHandler) DoSysLogout(ctx kim.Context) {
	logger.CometLogger.WithField("func", "DoSysLogout").Infof("do Logout of %s %s ", ctx.Session().GetChannelId(), ctx.Session().GetAccount())

	err := ctx.Delete(ctx.Session().GetAccount(), ctx.Session().GetChannelId())
	if err != nil {
		_ = ctx.RespWithError(pkt.Status_SystemException, err)
		return
	}

	_ = ctx.Resp(pkt.Status_Success, nil)
}
func (h *LoginHandler) ValidUser(session pkt.Session, ctx kim.Context) error {
	if session.AccessToken != "" {
		result, _ := ctx.RedisGet(session.Account)
		if result != session.AccessToken {
			return errors.New("AccessToken过期")
		}
		return nil
	} else if session.Password != "" {
		err := h.userService.Login(ctx.Session().GetApp(), &rpc.LoginReq{
			Account:  session.Account,
			Password: session.Password,
		})
		if err != nil {
			return err
		}
	}
	return errors.New("请重新登录")
}

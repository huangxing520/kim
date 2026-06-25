// 文件：login_handler.go
// 职责：登录/登出处理——处理客户端 SignIn/SignOut，含用户验证、Session 管理、异地登录踢下线。
//
// 定义的类型：
//   - LoginHandler 结构体：登录处理器（持有 User service）
//
// 方法：
//   - NewLoginHandler(user)             → 创建 LoginHandler
//   - (LoginHandler).DoSysLogin(ctx)     → 处理登录：验证用户 → 检查异地登录 → 踢下线 → 添加 Session → 返回成功
//   - (LoginHandler).DoSysLogout(ctx)    → 处理登出：删除 Session → 返回成功
//   - (LoginHandler).ValidUser(session, ctx) → 验证用户身份（AccessToken / Password 两种方式）

package handler

import (
	"errors"
	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/gen/rpc"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/services/comet/service"
	"github.com/klintcheng/kim/wire/pkt"
)

// LoginHandler 登录处理器
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

	err := h.ValidUser(&session, ctx)
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
func (h *LoginHandler) ValidUser(session *pkt.Session, ctx kim.Context) error {
	if session.AccessToken != "" {
		result, _ := ctx.RedisGet(session.Account)
		if result != session.AccessToken {
			return errors.New("AccessToken过期")
		}
		return nil
	} else if session.Password != "" {
		err := h.userService.Login(ctx.StdContext(), ctx.Session().GetApp(), &rpc.LoginReq{
			Account:  session.Account,
			Password: session.Password,
		})
		if err != nil {
			return err
		}
	}
	return errors.New("请重新登录")
}

// 文件：handler.go
// 职责：Gateway 业务处理器——实现 Acceptor / MessageListener / StateListener 接口，
//       处理客户端登录鉴权、消息转发、断线登出。
//
// 常量：
//   - MetaKeyApp / MetaKeyAccount：Meta 中的 App 和 Account key
//
// 定义的类型：
//   - Handler 结构体：Gateway 业务处理器（持有 ServiceID 和 AppSecret）
//
// 方法：
//   - (Handler).Accept(conn, timeout)    → 连接接收：读取登录包 → JWT 验证 → 生成 ChannelID → Forward 到 Login 服务
//   - (Handler).Receive(ag, payload)     → 消息接收：Ping→Pong 处理 / LogicPkt Forward 到对应服务
//   - (Handler).Disconnect(id)           → 连接断开：Forward SignOut 到 Login 服务
//   - getIP(remoteAddr)                  → 从远程地址字符串中提取 IP（去掉端口）
//   - generateChannelID(serviceID, account) → 生成全局唯一 ChannelID

package serv

import (
	"bytes"
	"fmt"
	"strings" // 【修复#3】新加的：用 strings.LastIndex 替代正则表达式
	"time"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/container"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/wire"
	"github.com/klintcheng/kim/wire/pkt"
	"github.com/klintcheng/kim/wire/token"
)

// Meta Key 常量
const (
	MetaKeyApp     = "app"
	MetaKeyAccount = "account"
)

// Handler Gateway 业务处理器
type Handler struct {
	ServiceID string
	AppSecret string
}

// Accept this connection
func (h *Handler) Accept(conn kim.Conn, timeout time.Duration) (string, kim.Meta, error) {
	// 1. 读取登录包
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	frame, err := conn.ReadFrame()
	if err != nil {
		return "", nil, err
	}

	buf := bytes.NewBuffer(frame.GetPayload())
	req, err := pkt.MustReadLogicPkt(buf)
	if err != nil {
		logger.GatewayLogger.WithFields(logger.Fields{
			"service": "gateway",
			"pkg":     "serv",
		}).Error(err)
		return "", nil, err
	}
	// 2. 必须是登录包
	if req.Command != wire.CommandLoginSignIn {
		resp := pkt.NewFrom(&req.Header)
		resp.Status = pkt.Status_InvalidCommand
		_ = conn.WriteFrame(kim.OpBinary, pkt.Marshal(resp))
		return "", nil, fmt.Errorf("must be a SignIn command")
	}

	// 3. 反序列化Body
	var login pkt.LoginReq
	err = req.ReadBody(&login)
	if err != nil {
		return "", nil, err
	}
	secret := h.AppSecret
	if secret == "" {
		secret = token.DefaultSecret
	}

	//err = wsutil.WriteServerMessage(conn, ws.OpText, []byte("12345"))
	//if err != nil {
	//	log.Println(err)
	//}
	// 4. 使用默认的DefaultSecret 解析token
	tk, err := token.Parse(secret, login.Token)
	if err != nil {
		// 5. 如果token无效，就返回SDK一个Unauthorized消息
		resp := pkt.NewFrom(&req.Header)
		resp.Status = pkt.Status_Unauthorized
		err1 := conn.WriteFrame(kim.OpBinary, pkt.Marshal(resp))
		print(err1)
		return "", nil, err
	}
	// 6. 生成一个全局唯一的ChannelID
	id := generateChannelID(h.ServiceID, tk.Account)
	logger.GatewayLogger.WithFields(logger.Fields{
		"service": "gateway",
		"pkg":     "serv",
	}).Infof("accept %v channel:%s", tk, id)

	req.ChannelId = id
	req.WriteBody(&pkt.Session{
		Account:     tk.Account,
		ChannelId:   id,
		GateId:      h.ServiceID,
		Password:    tk.Password,
		App:         tk.App,
		AccessToken: tk.AccessToken,
		RemoteIP:    getIP(conn.RemoteAddr().String()),
	})
	req.AddStringMeta(MetaKeyApp, tk.App)
	req.AddStringMeta(MetaKeyAccount, tk.Account)

	// 7. 把login.转发给Login服务
	err = container.Forward(wire.SNLogin, req)
	if err != nil {
		logger.GatewayLogger.WithFields(logger.Fields{
			"service": "gateway",
			"pkg":     "serv",
		}).Errorf("container.Forward :%v", err)
		return "", nil, err
	}
	return id, kim.Meta{
		MetaKeyApp:     tk.App,
		MetaKeyAccount: tk.Account,
	}, nil
}

// Receive default listener
func (h *Handler) Receive(ag kim.Agent, payload []byte) {
	buf := bytes.NewBuffer(payload)
	packet, err := pkt.Read(buf)
	if err != nil {
		logger.GatewayLogger.WithFields(logger.Fields{
			"service": "gateway",
			"pkg":     "serv",
		}).Error(err)
		return
	}

	if logicPkt, ok := packet.(*pkt.LogicPkt); ok {
		logicPkt.ChannelId = ag.ID()

		messageInTotal.WithLabelValues(h.ServiceID, wire.SNTGateway, logicPkt.Command).Inc()
		messageInFlowBytes.WithLabelValues(h.ServiceID, wire.SNTGateway, logicPkt.Command).Add(float64(len(payload)))

		// 把meta注入到header中
		if ag.GetMeta() != nil {
			logicPkt.AddStringMeta(MetaKeyApp, ag.GetMeta()[MetaKeyApp])
			logicPkt.AddStringMeta(MetaKeyAccount, ag.GetMeta()[MetaKeyAccount])
		}

		err = container.Forward(logicPkt.ServiceName(), logicPkt)
		if err != nil {
			logger.GatewayLogger.WithFields(logger.Fields{
				"module": "handler",
				"id":     ag.ID(),
				"cmd":    logicPkt.Command,
				"dest":   logicPkt.Dest,
			}).Error(err)
		}
	}

}

// Disconnect default listener
func (h *Handler) Disconnect(id string) error {
	logger.GatewayLogger.WithFields(logger.Fields{
		"service": "gateway",
		"pkg":     "serv",
	}).Infof("disconnect %s", id)

	logout := pkt.New(wire.CommandLoginSignOut, pkt.WithChannel(id))
	err := container.Forward(wire.SNLogin, logout)
	if err != nil {
		logger.GatewayLogger.WithFields(logger.Fields{
			"module": "handler",
			"id":     id,
		}).Error(err)
	}
	return nil
}

// 【修复#3】去掉原 var ipExp = regexp.MustCompile(string("\\:[0-9]+$"))
// 原代码使用正则表达式匹配末尾的端口号，每次调用 ReplaceAllString 都会分配新字符串
// 且正则匹配开销高于简单字符串操作
// 新加的：使用 strings.LastIndex 实现相同功能，避免正则开销

func getIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	// 【修复#3】新加的：用 strings.LastIndex 找最后一个冒号，截取 IP 部分
	// 原代码：return ipExp.ReplaceAllString(remoteAddr, "")
	// 新逻辑等价于去掉末尾 ":port"，且对 IPv6 也更安全（取最后一个冒号）
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return remoteAddr[:idx] // 新加的：截取冒号前的部分
	}
	return remoteAddr
}

func generateChannelID(serviceID, account string) string {
	return fmt.Sprintf("%s_%s_%d", serviceID, account, wire.Seq.Next())
}

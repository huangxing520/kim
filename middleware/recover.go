// 文件：recover.go
// 职责：Panic 恢复中间件——捕获 handler 执行中的 panic，记录调用栈日志并返回系统异常响应。
//
// 方法：
//   - Recover() → 返回一个 HandlerFunc 中间件，defer recover 捕获 panic 后记录调用栈并响应 SystemException

package middleware

import (
	"fmt"
	"runtime"
	"strings"

	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/wire/pkt"
)

// Recover 返回 panic 恢复中间件
func Recover() kim.HandlerFunc {
	return func(ctx kim.Context) {
		defer func() {
			if err := recover(); err != nil {
				var callers []string
				for i := 1; ; i++ {
					_, file, line, got := runtime.Caller(i)
					if !got {
						break
					}
					callers = append(callers, fmt.Sprintf("%s:%d", file, line))
				}
			
			logger.CometLogger.Errorw(fmt.Sprintf("%v", err),"ChannelId",ctx.Header().ChannelId,
					"Command",ctx.Header().Command,
					"Seq",ctx.Header().Sequence,"Caller",strings.Join(callers, "\n"))
				
				_ = ctx.Resp(pkt.Status_SystemException, &pkt.ErrorResp{Message: "SystemException"})
			}
		}()

		ctx.Next()
	}

}

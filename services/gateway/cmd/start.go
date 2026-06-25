// 文件：start.go
// 职责：Gateway 服务 Cobra 子命令——解析命令行参数，加载配置，创建并启动 Gateway 服务。
//
// 方法：
//   - NewStartCmd(ctx, version) → 创建 gateway 子命令，支持 --config/-c、--route/-r、--protocol/-p 参数

package cmd

import (
	"context"
	"time"

	"github.com/klintcheng/kim/services/gateway"
	"github.com/spf13/cobra"
)

// NewStartCmd 创建 gateway 子命令
func NewStartCmd(ctx context.Context, version string) *cobra.Command {
	var (
		configPath string
		routePath  string
		protocol   string
	)

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway service (WS/TCP + gRPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := gateway.LoadConfig(configPath)
			if err != nil {
				return err
			}

			srv, err := gateway.New(ctx, cfg, routePath, protocol)
			if err != nil {
				return err
			}

			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Stop(shutdownCtx)
			}()

			return srv.Start(ctx)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c",
		"services/gateway/conf.yaml", "config file")
	cmd.Flags().StringVarP(&routePath, "route", "r",
		"services/gateway/route.json", "route file")
	cmd.Flags().StringVarP(&protocol, "protocol", "p",
		"ws", "protocol of ws or tcp")

	return cmd
}

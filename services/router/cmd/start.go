// 文件：start.go
// 职责：Router 服务 Cobra 子命令——解析命令行参数，加载配置，创建并启动 Router 服务（HTTP/Iris）。
//
// 方法：
//   - NewStartCmd(ctx, version) → 创建 router 子命令，支持 --config/-c、--data/-d 参数

package cmd

import (
	"context"
	"time"

	"github.com/klintcheng/kim/services/router"
	"github.com/spf13/cobra"
)

// NewStartCmd 创建 router 子命令
func NewStartCmd(ctx context.Context, version string) *cobra.Command {
	var configPath string
	var dataPath string

	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the router service (HTTP server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := router.LoadConfig(configPath)
			if err != nil {
				return err
			}

			srv, err := router.New(ctx, cfg, dataPath)
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
		"services/router/conf.yaml", "config file")
	cmd.Flags().StringVarP(&dataPath, "data", "d",
		"services/router/data", "data path")

	return cmd
}

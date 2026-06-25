package cmd

import (
	"context"
	"time"

	"github.com/klintcheng/kim/services/comet"
	"github.com/spf13/cobra"
)

// NewStartCmd 创建 comet 子命令
func NewStartCmd(ctx context.Context, version string) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "comet",
		Short: "Start the comet service (gRPC server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := comet.LoadConfig(configPath)
			if err != nil {
				return err
			}

			srv, err := comet.New(ctx, cfg)
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
		"services/comet/conf.yaml", "config file")

	return cmd
}

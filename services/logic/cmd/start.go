package cmd

import (
	"context"
	"time"

	"github.com/klintcheng/kim/services/logic"
	"github.com/spf13/cobra"
)

func NewStartCmd(ctx context.Context, version string) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "logic",
		Short: "Start the logic service (gRPC server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := logic.LoadConfig(configPath)
			if err != nil {
				return err
			}

			srv, err := logic.New(ctx, cfg)
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
		"services/logic/conf.yaml", "config file")

	return cmd
}

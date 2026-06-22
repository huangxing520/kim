package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/klintcheng/kim/services/comet"
	"github.com/klintcheng/kim/services/gateway"
	"github.com/klintcheng/kim/services/logic"
	"github.com/klintcheng/kim/services/router"
	"github.com/spf13/cobra"
)

const version = "v1"

func main() {
	flag.Parse()

	root := &cobra.Command{
		Use:     "kim",
		Version: version,
		Short:   "King IM Cloud",
	}
	ctx := context.Background()

	root.AddCommand(gateway.NewServerStartCmd(ctx, version))
	root.AddCommand(comet.NewServerStartCmd(ctx, version))
	root.AddCommand(logic.NewServerStartCmd(ctx, version))
	root.AddCommand(router.NewServerStartCmd(ctx, version))

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not run command: %v\n", err)
		os.Exit(1)
	}
}

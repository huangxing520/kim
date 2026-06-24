// 文件：main.go
// 职责：KIM 统一入口——Cobra 命令行主程序，注册 gateway/comet/logic/router 四个子命令。
//
// 方法：
//   - main() → 解析命令行参数，创建 root Cobra 命令，添加四个服务子命令后执行

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/klintcheng/kim/services/comet"
	"github.com/klintcheng/kim/services/gateway"
	logiccmd "github.com/klintcheng/kim/services/logic/cmd"
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
	root.AddCommand(logiccmd.NewStartCmd(ctx, version))
	root.AddCommand(router.NewServerStartCmd(ctx, version))

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not run command: %v\n", err)
		os.Exit(1)
	}
}

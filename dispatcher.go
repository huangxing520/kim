package kim

import (
	"context"

	"github.com/klintcheng/kim/wire/pkt"
)

type Dispatcher interface {
	Push(ctx context.Context, gateway string, channels []string, p *pkt.LogicPkt) error
}

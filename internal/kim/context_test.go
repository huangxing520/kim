package kim

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/klintcheng/kim/wire/pkt"
)

func TestContextImpl_StdContext(t *testing.T) {
	c := BuildContext().(*ContextImpl)

	ctx := c.StdContext()
	assert.NotNil(t, ctx)

	type ctxKey struct{}
	c2 := c.WithStdContext(context.WithValue(ctx, ctxKey{}, "test"))
	assert.Equal(t, "test", c2.StdContext().Value(ctxKey{}))
}

func TestContextImpl_StdContextNil(t *testing.T) {
	c := &ContextImpl{}
	ctx := c.StdContext()
	assert.NotNil(t, ctx)
}

func TestContextImpl_StdContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := BuildContext().(*ContextImpl).WithStdContext(ctx)

	cancel()
	select {
	case <-c.StdContext().Done():
	case <-time.After(time.Second):
		t.Fatal("context should be canceled")
	}
}

func TestContextImpl_Dispatch_MultiGatewayErrorAggregation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDispatcher := NewMockDispatcher(ctrl)

	selfChannel := "self-channel"
	selfGate := "gate-self"

	recv1 := &Location{ChannelId: "ch1", GateId: "gate1"}
	recv2 := &Location{ChannelId: "ch2", GateId: "gate1"}
	recv3 := &Location{ChannelId: "ch3", GateId: "gate2"}
	recvSelf := &Location{ChannelId: selfChannel, GateId: "gate1"}
	recvNil := (*Location)(nil)

	gate1Err := errors.New("gateway1 push error")

	mockDispatcher.EXPECT().
		Push(gomock.Any(), "gate1", []string{"ch1", "ch2"}, gomock.Any()).
		Return(gate1Err)
	mockDispatcher.EXPECT().
		Push(gomock.Any(), "gate2", []string{"ch3"}, gomock.Any()).
		Return(nil)

	c := &ContextImpl{
		Dispatcher:     mockDispatcher,
		SessionStorage: nil,
		request:        pkt.New("test.command"),
		session: &pkt.Session{
			ChannelId: selfChannel,
			GateId:    selfGate,
		},
		stdCtx: context.Background(),
	}

	err := c.Dispatch(&pkt.ErrorResp{Message: "test"}, recv1, recv2, recv3, recvSelf, recvNil)

	assert.Equal(t, gate1Err, err, "should return first error")
}

func TestContextImpl_Dispatch_NoReceivers(t *testing.T) {
	c := &ContextImpl{
		Dispatcher:     nil,
		SessionStorage: nil,
		request:        pkt.New("test.command"),
		session: &pkt.Session{
			ChannelId: "self",
			GateId:    "gate-self",
		},
		stdCtx: context.Background(),
	}

	err := c.Dispatch(&pkt.ErrorResp{Message: "test"})
	assert.NoError(t, err, "should return nil with no receivers")
}

func TestContextImpl_Dispatch_AllGatewaysFail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDispatcher := NewMockDispatcher(ctrl)

	gate1Err := errors.New("gateway1 error")
	gate2Err := errors.New("gateway2 error")

	mockDispatcher.EXPECT().
		Push(gomock.Any(), "gate1", []string{"ch1"}, gomock.Any()).
		Return(gate1Err)
	mockDispatcher.EXPECT().
		Push(gomock.Any(), "gate2", []string{"ch2"}, gomock.Any()).
		Return(gate2Err)

	c := &ContextImpl{
		Dispatcher:     mockDispatcher,
		SessionStorage: nil,
		request:        pkt.New("test.command"),
		session: &pkt.Session{
			ChannelId: "self",
			GateId:    "gate-self",
		},
		stdCtx: context.Background(),
	}

	err := c.Dispatch(&pkt.ErrorResp{Message: "test"},
		&Location{ChannelId: "ch1", GateId: "gate1"},
		&Location{ChannelId: "ch2", GateId: "gate2"},
	)

	assert.Equal(t, gate1Err, err, "should return the first error encountered")
}

func TestContextImpl_Dispatch_StdContextPropagates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDispatcher := NewMockDispatcher(ctrl)

	type ctxKey struct{}
	customCtx := context.WithValue(context.Background(), ctxKey{}, "custom-value")

	var capturedCtx context.Context
	mockDispatcher.EXPECT().
		Push(gomock.Any(), "gate1", []string{"ch1"}, gomock.Any()).
		DoAndReturn(func(ctx context.Context, gateway string, channels []string, p *pkt.LogicPkt) error {
			capturedCtx = ctx
			return nil
		})

	c := &ContextImpl{
		Dispatcher:     mockDispatcher,
		SessionStorage: nil,
		request:        pkt.New("test.command"),
		session: &pkt.Session{
			ChannelId: "self",
			GateId:    "gate-self",
		},
		stdCtx: customCtx,
	}

	err := c.Dispatch(&pkt.ErrorResp{Message: "test"},
		&Location{ChannelId: "ch1", GateId: "gate1"},
	)

	assert.NoError(t, err)
	assert.Equal(t, "custom-value", capturedCtx.Value(ctxKey{}), "StdContext should propagate to Push calls")
}

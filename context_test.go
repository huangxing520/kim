package kim

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContextImpl_StdContext(t *testing.T) {
	c := BuildContext().(*ContextImpl)

	ctx := c.StdContext()
	assert.NotNil(t, ctx)

	type ctxKey struct{}
	c2 := c.WithStdContext(context.WithValue(ctx, ctxKey{}, "test"))
	assert.Equal(t, "test", c2.StdContext().Value(ctxKey{}))
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

package client

import (
	"testing"
)

// TestPoolCloseIdempotent 验证 Pool.Close 可以多次调用不 panic
func TestPoolCloseIdempotent(t *testing.T) {
	p := NewPool(nil, "test-svc")
	p.Close()
	p.Close()
}

// TestPoolCloseWithoutNaming 验证 nil naming 时 Close 不 panic
func TestPoolCloseWithoutNaming(t *testing.T) {
	p := NewPool(nil, "test-svc")
	p.Close()
}

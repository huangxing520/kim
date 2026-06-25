package server

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRecoveryInterceptor_PanicRecovery(t *testing.T) {
	panicHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("test panic")
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/TestMethod"}
	resp, err := RecoveryInterceptor(context.Background(), nil, info, panicHandler)

	if resp != nil {
		t.Errorf("expected nil response on panic, got %v", resp)
	}

	if err == nil {
		t.Fatal("expected error on panic, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}

	if st.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", st.Code())
	}

	if st.Message() != "internal server error" {
		t.Errorf("expected generic 'internal server error' message, got %q", st.Message())
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "goroutine") {
		t.Errorf("error should not contain goroutine stack trace, got: %s", errMsg)
	}
	if strings.Contains(errMsg, ".go:") {
		t.Errorf("error should not contain file:line stack references, got: %s", errMsg)
	}
	if strings.Contains(errMsg, "runtime/debug.Stack") {
		t.Errorf("error should not contain stack trace internals, got: %s", errMsg)
	}
}

func TestRecoveryInterceptor_NilPanic(t *testing.T) {
	nilPanicHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
		var f func()
		f()
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/TestMethod"}
	resp, err := RecoveryInterceptor(context.Background(), nil, info, nilPanicHandler)

	if resp != nil {
		t.Errorf("expected nil response on nil pointer panic")
	}

	if err == nil {
		t.Fatal("expected error on nil pointer panic")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error")
	}

	if st.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", st.Code())
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "goroutine") {
		t.Errorf("error should not leak stack trace")
	}
}

func TestRecoveryInterceptor_NoPanic(t *testing.T) {
	normalHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/TestMethod"}
	resp, err := RecoveryInterceptor(context.Background(), "request", info, normalHandler)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if resp != "ok" {
		t.Errorf("expected 'ok' response, got %v", resp)
	}
}

func TestUnaryChain_Empty(t *testing.T) {
	chain := UnaryChain()
	if chain != nil {
		t.Error("empty chain should return nil interceptor")
	}
}

func TestUnaryChain_Single(t *testing.T) {
	called := false
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		called = true
		return handler(ctx, req)
	}

	chain := UnaryChain(interceptor)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}
	resp, err := chain(context.Background(), nil, info, handler)

	if !called {
		t.Error("interceptor should have been called")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp != "ok" {
		t.Errorf("expected 'ok', got %v", resp)
	}
}

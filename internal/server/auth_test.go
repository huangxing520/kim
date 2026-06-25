package server

import (
	"context"
	"testing"
	"time"

	"github.com/klintcheng/kim/wire/token"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	_, err := interceptor(context.Background(), nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_ValidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	validToken, err := token.Generate(secret, &token.Token{
		Account: "testuser",
		App:     "kim",
		Exp:     time.Now().Add(time.Hour).Unix(),
	})
	assert.Nil(t, err)

	md := metadata.New(map[string]string{"authorization": "Bearer " + validToken})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, nil, info, handler)
	assert.Nil(t, err)
	assert.Equal(t, "ok", resp)
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	secret := "test-secret-32bytes-minimum!"
	interceptor := AuthInterceptor(secret)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/InsertUserMessage"}

	md := metadata.New(map[string]string{"authorization": "Bearer invalid.token.here"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, info, handler)
	assert.NotNil(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_HealthCheckBypass(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}

	_, err := interceptor(context.Background(), nil, info, handler)
	assert.Nil(t, err)
}

func TestAuthInterceptor_LoginBypass(t *testing.T) {
	interceptor := AuthInterceptor("test-secret-32bytes-minimum!")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/rpc.LogicService/Login"}

	resp, err := interceptor(context.Background(), nil, info, handler)
	assert.Nil(t, err)
	assert.Equal(t, "ok", resp)
}

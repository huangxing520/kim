package server

import (
	"context"
	"strings"

	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/wire/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var authBypassMethods = map[string]bool{
	"/grpc.health.v1.Health/Check": true,
	"/rpc.LogicService/Login":      true,
}

func AuthInterceptor(secret string) UnaryInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if authBypassMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md["authorization"]
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}

		tokenStr := values[0]
		if strings.HasPrefix(strings.ToLower(tokenStr), "bearer ") {
			tokenStr = tokenStr[7:]
		}

		_, err := token.Parse(secret, tokenStr)
		if err != nil {
			logger.CommonLogger.Warnf("auth failed for %s: %v", info.FullMethod, err)
			return nil, status.Errorf(codes.Unauthenticated, "invalid token")
		}

		return handler(ctx, req)
	}
}

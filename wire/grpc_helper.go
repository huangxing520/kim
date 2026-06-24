// 文件：grpc_helper.go
// 职责：gRPC 辅助函数——判断错误是否为指定 gRPC 状态码。
//
// 方法：
//   - IsGrpcError(err, code) → 判断 err 是否为指定的 gRPC status code

package wire

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsGrpcError 判断 err 是否为指定的 gRPC 状态码
func IsGrpcError(err error, code codes.Code) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == code
	}
	return false
}

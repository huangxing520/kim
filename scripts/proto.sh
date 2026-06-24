#!/bin/bash
set -e

PROTO_ROOT="api/proto"
GEN_ROOT="gen"

# 清空旧生成代码
rm -rf ${GEN_ROOT}/pkt ${GEN_ROOT}/rpc
mkdir -p ${GEN_ROOT}/pkt ${GEN_ROOT}/rpc

# 生成客户端协议（pkt）
protoc \
  --proto_path=${PROTO_ROOT}/pkt \
  --go_out=${GEN_ROOT}/pkt \
  --go_opt=paths=source_relative \
  ${PROTO_ROOT}/pkt/*.proto

# 生成服务间协议（rpc，含 gRPC service）
protoc \
  --proto_path=${PROTO_ROOT}/rpc \
  --proto_path=${PROTO_ROOT}/pkt \
  --go_out=${GEN_ROOT}/rpc \
  --go_opt=paths=source_relative \
  --go-grpc_out=${GEN_ROOT}/rpc \
  --go-grpc_opt=paths=source_relative \
  ${PROTO_ROOT}/rpc/*.proto

echo "proto generated to ${GEN_ROOT}/"

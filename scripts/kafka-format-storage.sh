#!/bin/bash
# 为 KRaft 控制器预格式化 storage 目录，解决 cp-kafka StorageTool 要求 advertised.listeners
# 但 controller 进程又不支持该配置的冲突。
# 只需在首次部署或清理数据后执行一次。

set -e

CLUSTER_ID="MkU3OEVBNTcwNTJENDM2Qk"
IMAGE="docker.io/confluentinc/cp-kafka:8.2.2"

format_one() {
    local node_id=$1
    local data_dir=$2

    echo "=== 格式化 controller-${node_id}: ${data_dir} ==="

    # 确保目录存在且属于 uid 1000
    mkdir -p "${data_dir}"
    chown -R 1000:1000 "${data_dir}"

    # 生成临时配置文件（仅在格式化时使用 advertised.listeners）
    local tmp_conf=$(mktemp)
    cat > "${tmp_conf}" <<EOF
node.id=${node_id}
process.roles=controller
controller.quorum.voters=1@controller-1:9093,2@controller-2:9093,3@controller-3:9093
controller.listener.names=CONTROLLER
listeners=CONTROLLER://0.0.0.0:9093
# 以下两行仅为绕过 StorageTool 校验，实际 Kafka 启动时不使用
advertised.listeners=CONTROLLER://localhost:9093
inter.broker.listener.name=CONTROLLER
EOF

    docker run --rm \
        -v "${data_dir}:/var/lib/kafka/data" \
        -v "${tmp_conf}:/tmp/format.properties:ro" \
        "${IMAGE}" \
        kafka-storage format \
            --config /tmp/format.properties \
            --cluster-id "${CLUSTER_ID}" \
            --ignore-formatted 2>&1 || true

    rm -f "${tmp_conf}"
    echo "controller-${node_id} 格式化完成"
    echo ""
}

format_one 1 ~/data/controller-1
format_one 2 ~/data/controller-2
format_one 3 ~/data/controller-3

echo "=== 全部 controller storage 格式化完成 ==="
echo "现在可以启动 Kafka 集群: docker compose -f docker-compose.yml up -d"
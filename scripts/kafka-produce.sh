#!/bin/bash
# 本地向 Docker Kafka 集群生产消息的脚本
# 用法:
#   交互模式:          ./scripts/kafka-produce.sh
#   指定 topic:        ./scripts/kafka-produce.sh -t my_topic
#   发送单条消息:       ./scripts/kafka-produce.sh -m "hello kafka"
#   发送多条消息(逗号分隔): ./scripts/kafka-produce.sh -m "msg1,msg2,msg3"
#   从文件读取:         ./scripts/kafka-produce.sh -f messages.txt
#   生成测试数据:       ./scripts/kafka-produce.sh --gen 100
#
# 默认 broker: broker-1:19092（容器内部地址）
# 默认 topic:  kim_logs

set -e

BROKER="broker-1:19092"
TOPIC="kim_logs"
CONTAINER="kim_broker_1"
MESSAGE=""
FILE=""
GEN_COUNT=""
KAFKA_BIN="/opt/kafka/bin"

usage() {
    cat <<EOF
用法: $0 [选项]

选项:
  -t, --topic TOPIC      指定 topic（默认: kim_logs）
  -b, --broker BROKER    指定 broker 地址（默认: broker-1:19092）
  -c, --container NAME   指定容器名（默认: kim_broker_1）
  -m, --message MSG      发送消息（多条用逗号分隔）
  -f, --file FILE        从文件读取消息（每行一条）
  --gen N                生成 N 条测试消息
  -h, --help             显示帮助

示例:
  # 交互模式，逐行输入消息（Ctrl+D 结束）
  $0

  # 发送单条消息
  $0 -m "hello world"

  # 发送多条消息
  $0 -m "msg1,msg2,msg3"

  # 生成 100 条测试消息
  $0 --gen 100

  # 从文件发送
  $0 -f /tmp/messages.txt
EOF
    exit 0
}

# 解析参数
while [[ $# -gt 0 ]]; do
    case "$1" in
        -t|--topic)    TOPIC="$2"; shift 2 ;;
        -b|--broker)   BROKER="$2"; shift 2 ;;
        -c|--container) CONTAINER="$2"; shift 2 ;;
        -m|--message)  MESSAGE="$2"; shift 2 ;;
        -f|--file)     FILE="$2"; shift 2 ;;
        --gen)         GEN_COUNT="$2"; shift 2 ;;
        -h|--help)     usage ;;
        *)             echo "未知选项: $1"; usage ;;
    esac
done

# 检查容器是否运行
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
    echo "错误: 容器 '${CONTAINER}' 未运行，请先启动 Kafka 集群:"
    echo "  docker compose -f docker-compose.yml up -d"
    exit 1
fi

echo ">>> Broker:  $BROKER"
echo ">>> Topic:   $TOPIC"
echo ">>> 容器:    $CONTAINER"
echo ""

# 模式 1: 生成测试消息
if [[ -n "$GEN_COUNT" ]]; then
    echo ">>> 生成 ${GEN_COUNT} 条测试消息..."
    for i in $(seq 1 "$GEN_COUNT"); do
        echo "{\"seq\": $i, \"msg\": \"test message $i\", \"ts\": \"$(date +%Y-%m-%dT%H:%M:%S)\"}"
    done | docker exec -i "$CONTAINER" \
        "${KAFKA_BIN}/kafka-console-producer.sh" \
        --bootstrap-server "$BROKER" \
        --topic "$TOPIC"
    echo ">>> 完成！已发送 ${GEN_COUNT} 条消息到 ${TOPIC}"
    exit 0
fi

# 模式 2: 命令行消息
if [[ -n "$MESSAGE" ]]; then
    echo ">>> 发送消息..."
    # 将逗号分隔的消息转为换行分隔
    echo "$MESSAGE" | tr ',' '\n' | docker exec -i "$CONTAINER" \
        "${KAFKA_BIN}/kafka-console-producer.sh" \
        --bootstrap-server "$BROKER" \
        --topic "$TOPIC"
    echo ">>> 完成！消息已发送到 ${TOPIC}"
    exit 0
fi

# 模式 3: 从文件读取
if [[ -n "$FILE" ]]; then
    if [[ ! -f "$FILE" ]]; then
        echo "错误: 文件 '$FILE' 不存在"
        exit 1
    fi
    echo ">>> 从文件 '$FILE' 发送消息..."
    docker exec -i "$CONTAINER" \
        "${KAFKA_BIN}/kafka-console-producer.sh" \
        --bootstrap-server "$BROKER" \
        --topic "$TOPIC" < "$FILE"
    echo ">>> 完成！消息已发送到 ${TOPIC}"
    exit 0
fi

# 模式 4: 交互模式
echo ">>> 交互模式：逐行输入消息，按 Ctrl+D 结束发送"
echo ""
docker exec -it "$CONTAINER" \
    "${KAFKA_BIN}/kafka-console-producer.sh" \
    --bootstrap-server "$BROKER" \
    --topic "$TOPIC"
echo ""
echo ">>> 交互模式结束"
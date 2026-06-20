package logger

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/sirupsen/logrus"
)

// KafkaHook 是一个基于 logrus.Hook 的异步缓冲 Kafka 日志写入器。
// Fire 将日志非阻塞地推入带缓冲 channel，后台 goroutine 消费并写入 Kafka。
type KafkaHook struct {
	writer    *kafka.Writer
	ch        chan []byte
	formatter logrus.Formatter
	dropCount int64
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// newKafkaHook 创建一个 KafkaHook 并启动后台消费 goroutine。
func newKafkaHook(ks KafkaSettings) (*KafkaHook, error) {
	if len(ks.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers is empty")
	}
	if ks.Topic == "" {
		return nil, fmt.Errorf("kafka topic is empty")
	}

	bufSize := ks.BufferSize
	if bufSize <= 0 {
		bufSize = 1024
	}

	timeout := 10 * time.Second
	if ks.Timeout != "" {
		if d, err := time.ParseDuration(ks.Timeout); err == nil {
			timeout = d
		}
	}

	// 确保 topic 存在（集群可能关闭了 auto.create.topics.enable）
	if err := ensureTopic(ks); err != nil {
		fmt.Fprintf(os.Stderr, "[kafkahook] ensure topic warning: %v\n", err)
	}

	w := &kafka.Writer{
		Addr:         kafka.TCP(ks.Brokers...),
		Topic:        ks.Topic,
		WriteTimeout: timeout,
		Balancer:     &kafka.LeastBytes{},
	}

	h := &KafkaHook{
		writer: w,
		ch:     make(chan []byte, bufSize),
		formatter: &logrus.JSONFormatter{
			DisableTimestamp: false,
		},
	}

	h.wg.Add(1)
	go h.run()
	return h, nil
}

// Levels 返回 Hook 触发的日志级别（Info ~ Panic，过滤掉 Trace/Debug 噪声）。
func (h *KafkaHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
	}
}

// Fire 将日志条目格式化为 JSON 后非阻塞推入 channel。
// channel 满时丢弃该条日志并累加丢弃计数，向 stderr 输出告警。
func (h *KafkaHook) Fire(entry *logrus.Entry) error {
	data, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}
	select {
	case h.ch <- data:
	default:
		n := atomic.AddInt64(&h.dropCount, 1)
		if n%100 == 1 {
			fmt.Fprintf(os.Stderr, "[kafkahook] log dropped, total dropped: %d\n", n)
		}
	}
	return nil
}

// run 后台 goroutine：消费 channel 并将日志写入 Kafka。
func (h *KafkaHook) run() {
	defer h.wg.Done()
	for data := range h.ch {
		err := h.writer.WriteMessages(context.Background(), kafka.Message{
			Value: data,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kafkahook] write to kafka error: %v\n", err)
		}
	}
}

// Close 关闭 channel、等待后台 goroutine 退出（flush 剩余日志）、关闭 Kafka Writer。
func (h *KafkaHook) Close() error {
	var err error
	h.closeOnce.Do(func() {
		close(h.ch)
		h.wg.Wait()
		err = h.writer.Close()
	})
	return err
}

// ensureTopic 检查 topic 是否存在，不存在则创建。
// 适用于集群关闭了 auto.create.topics.enable 的场景。
func ensureTopic(ks KafkaSettings) error {
	replicationFactor := ks.ReplicationFactor
	if replicationFactor <= 0 {
		replicationFactor = 3
	}
	partitions := ks.Partitions
	if partitions <= 0 {
		partitions = 3
	}

	conn, err := kafka.DialLeader(context.Background(), "tcp", ks.Brokers[0], ks.Topic, 0)
	if err == nil {
		conn.Close()
		return nil // topic 已存在
	}

	// topic 不存在，尝试创建
	dialer := &kafka.Dialer{Timeout: 10 * time.Second}
	c, err := dialer.DialContext(context.Background(), "tcp", ks.Brokers[0])
	if err != nil {
		return fmt.Errorf("dial kafka broker failed: %w", err)
	}
	defer c.Close()

	controller, err := c.Controller()
	if err != nil {
		return fmt.Errorf("get controller failed: %w", err)
	}

	controllerConn, err := dialer.DialContext(context.Background(), "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("dial controller failed: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             ks.Topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		return fmt.Errorf("create topic %s failed: %w", ks.Topic, err)
	}

	fmt.Fprintf(os.Stderr, "[kafkahook] created topic %s (partitions=%d, replication=%d)\n", ks.Topic, partitions, replicationFactor)
	return nil
}

package logger

import (
	"sync"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
)

// KafkaSettings Kafka 日志写入的可选配置。
type KafkaSettings struct {
	Enable            bool
	Brokers           []string
	Topic             string
	BufferSize        int    // channel 缓冲大小，默认 1024
	Timeout           string // Kafka 写超时，默认 "10s"
	ReplicationFactor int    // topic 副本数，默认 3
	Partitions        int    // topic 分区数，默认 3
}

type Settings struct {
	Filename    string
	Level       string
	RollingDays uint
	Format      string
	Kafka       KafkaSettings
}

// kafkaHookInst 保存 Init 时创建的 KafkaHook 实例，供 Close 使用。
var kafkaHookInst *KafkaHook

// initOnce 确保 Init 只执行一次，防止重复 AddHook 导致日志重复写入
var initOnce sync.Once

func Init(settings Settings) error {
	var initErr error
	initOnce.Do(func() {
		initErr = doInit(settings)
	})
	return initErr
}

func doInit(settings Settings) error {
	if settings.Level == "" {
		settings.Level = "debug"
	}
	ll, err := logrus.ParseLevel(settings.Level)
	if err == nil {
		std.SetLevel(ll)
	} else {
		std.Error("Invalid log level")
	}

	// 开启调用位置信息，方便排查问题
	std.SetReportCaller(true)

	if settings.Filename == "" {
		return nil
	}

	if settings.RollingDays == 0 {
		settings.RollingDays = 7
	}

	writer, err := rotatelogs.New(
		settings.Filename+".%Y%m%d",
		// WithLinkName为最新的日志建立软连接，以方便随着找到当前日志文件
		rotatelogs.WithLinkName(settings.Filename),

		// WithRotationTime设置日志分割的时间
		rotatelogs.WithRotationTime(time.Hour*24),

		// WithMaxAge和WithRotationCount二者只能设置一个，
		// WithMaxAge设置文件清理前的最长保存时间，
		// WithRotationCount设置文件清理前最多保存的个数。
		//rotatelogs.WithMaxAge(time.Hour*24),
		rotatelogs.WithRotationCount(settings.RollingDays),
	)
	if err != nil {
		return err
	}

	var logfr logrus.Formatter
	if settings.Format == "json" {
		logfr = &logrus.JSONFormatter{
			DisableTimestamp: false,
		}
	} else {
		logfr = &logrus.TextFormatter{
			DisableColors: true,
		}
	}

	lfsHook := lfshook.NewHook(lfshook.WriterMap{
		logrus.DebugLevel: writer,
		logrus.InfoLevel:  writer,
		logrus.WarnLevel:  writer,
		logrus.ErrorLevel: writer,
		logrus.FatalLevel: writer,
		logrus.PanicLevel: writer,
	}, logfr)

	std.AddHook(lfsHook)

	// 可选：启用 Kafka 日志写入
	if settings.Kafka.Enable && len(settings.Kafka.Brokers) > 0 && settings.Kafka.Topic != "" {
		hook, err := newKafkaHook(settings.Kafka)
		if err != nil {
			return err
		}
		kafkaHookInst = hook
		std.AddHook(hook)
	}
	return nil
}

// Close 关闭日志写入器（如 KafkaHook），应在服务退出时调用。
func Close() error {
	if kafkaHookInst != nil {
		return kafkaHookInst.Close()
	}
	return nil
}

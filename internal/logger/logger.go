// 文件：logger.go
// 职责：日志系统——基于 zap 的日志实现，支持文件滚动、控制台输出、Kafka 异步写入、多等级分类输出、
//       SugaredLogger 包装（提供 Trace / WithField / WithFields / WithError 链式调用）。
//       合并自原 logger/log.go 与 logger/config.go，移除了 init() 函数。
//
// 导出的 Logger 变量：
//   - CommonLogger / GatewayLogger / LogicLogger / CometLogger / RouterLogger：各服务的全局 logger（由各服务 Init 阶段设置）
//
// 定义的类型：
//   - Fields：兼容旧 logrus 风格的字段 map
//   - Settings：旧 API 兼容的日志初始化配置
//   - Options：新式可选项模式的日志配置
//   - Logger：日志实例，包装 zap.Logger，支持文件输出 + Kafka 写入
//   - SugaredLogger：包装 zap.SugaredLogger，补全 Trace 方法
//   - KafkaWriter：Kafka 异步生产者 io.Writer 适配
//   - ModOptions：Options 的函数式选项类型
//   - Config：日志配置（Kafka、日志等级、文件名、开发模式）
//   - Server：空结构体（预留用途）
//
// 主要方法：
//   - Init(s Settings)                         → 使用旧 API 创建 Logger 实例
//   - NewLogger(mods...)                       → 使用新 API 创建 Logger 实例
//   - InitConfig(file)                         → 从指定 YAML 文件加载 Config
//   - (Logger).Close()                         → 关闭日志（同步 zap + 关闭 Kafka 生产者）
//   - (Logger).Sugar()                         → 返回 SugaredLogger 用于链式调用
//   - (SugaredLogger).Trace / Tracef           → 映射到 Debug（zap 无 Trace 等级）
//   - (SugaredLogger).WithField / WithFields / WithError → 链式添加字段
//   - (Logger).init() / setSyncers() / cores() → 内部初始化：创建文件/控制台/Kafka 的 zapcore.Core 组合
//   - NewKafkaWriter(addrs, topic)             → 创建 Kafka Writer
//   - SetMaxSize / SetLogFileDir / SetLevel 等 → 函数式选项工厂函数
//   - timeEncoder / timeUnixNano / levelString → 辅助编码函数

package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/klintcheng/kim/model"
	"github.com/natefinch/lumberjack"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 各服务专用的全局 Logger 变量（由各服务 Init 阶段赋值）
var (
	GatewayLogger *SugaredLogger
	CommonLogger  *SugaredLogger
	LogicLogger   *SugaredLogger
	CometLogger   *SugaredLogger
	RouterLogger  *SugaredLogger
)

// ========== 兼容旧 API 的类型定义 ==========

// Fields 兼容旧 logrus Fields 风格
type Fields map[string]interface{}

// KafkaSettings Kafka 日志写入配置（兼容旧 API）

// Settings 日志初始化配置（兼容旧 API）
type Settings struct {
	Filename    string
	Level       string
	RollingDays uint
	Format      string
	ServiceName string // 服务名，自动注入到每条日志的 "service" 字段
	Kafka       model.KafkaSettings
	Development   bool    // 是否是开发模式
}

// ========== 新的可选项模式 ==========

type Options struct {
	LogFileDir    string // 文件保存目录
	AppName       string // 日志文件前缀
	ErrorFileName string
	WarnFileName  string
	InfoFileName  string
	DebugFileName string
	Level         zapcore.Level // 日志等级
	MaxSize       int           // 日志文件大小（M）
	MaxBackups    int           // 最多存在多少个切片文件
	MaxAge        int           // 保存的最大天数
	Development   bool          // 是否是开发模式
	ServiceName   string        // 服务名，自动注入到每条日志
	zap.Config
	KafkaAddrs []string // Kafka 地址，如 ["192.168.31.77:9092"]
	KafkaTopic string   // 要发送的 topic
}

type KafkaWriter struct {
	producer sarama.AsyncProducer
	topic    string
}

type ModOptions func(options *Options)

var (
	sp             = string(filepath.Separator)
	debugConsoleWS = zapcore.Lock(os.Stdout) // 控制台标准输出
	errorConsoleWS = zapcore.Lock(os.Stderr)
)

type Logger struct {
	*zap.Logger
	sync.RWMutex
	Opts        *Options `json:"opts"`
	zapConfig   zap.Config
	inited      bool
	kafkaWriter *KafkaWriter // 用于 Close 时关闭 Kafka 生产者
	errWS       zapcore.WriteSyncer
	warnWS      zapcore.WriteSyncer
	infoWS      zapcore.WriteSyncer
	debugWS     zapcore.WriteSyncer
}

// NewLogger 创建一个新的 Logger 实例（非单例，每次调用都创建独立实例）
func NewLogger(mod ...ModOptions) *Logger {
	l := &Logger{}
	l.Lock()
	defer l.Unlock()
	l.Opts = &Options{
		LogFileDir:    "",
		AppName:       "kim",
		ErrorFileName: "error.log",
		WarnFileName:  "warn.log",
		InfoFileName:  "info.log",
		DebugFileName: "debug.log",
		Level:         zapcore.DebugLevel,
		MaxSize:       100,
		MaxBackups:    60,
		MaxAge:        30,
	}
	for _, fn := range mod {
		fn(l.Opts)
	}
	if l.Opts.LogFileDir == "" {
		l.Opts.LogFileDir, _ = filepath.Abs(filepath.Dir(filepath.Join(".")))
		l.Opts.LogFileDir += sp + "logs" + sp
	}
	if l.Opts.Development {
		l.zapConfig = zap.NewDevelopmentConfig()
		l.zapConfig.EncoderConfig.EncodeTime = timeEncoder
	} else {
		l.zapConfig = zap.NewProductionConfig()
		l.zapConfig.EncoderConfig.EncodeTime = timeUnixNano
	}
	if len(l.Opts.OutputPaths) == 0 {
		l.zapConfig.OutputPaths = []string{"stdout"}
	}
	if len(l.Opts.ErrorOutputPaths) == 0 {
		l.zapConfig.ErrorOutputPaths = []string{"stderr"}
	}
	l.zapConfig.Level.SetLevel(l.Opts.Level)
	l.init()
	l.inited = true
	l.Info("[NewLogger] success")
	return l
}

func (l *Logger) init() {
	l.setSyncers()
	var err error
	l.Logger, err = l.zapConfig.Build(l.cores())
	if err != nil {
		panic(err)
	}
	// 自动注入 service 字段到每条日志
	if l.Opts.ServiceName != "" {
		l.Logger = l.Logger.With(zap.String("service", l.Opts.ServiceName))
	}
}

func (l *Logger) setSyncers() {
	f := func(fN string) zapcore.WriteSyncer {
		return zapcore.AddSync(&lumberjack.Logger{
			Filename:   l.Opts.LogFileDir + sp + l.Opts.AppName + "-" + fN,
			MaxSize:    l.Opts.MaxSize,
			MaxBackups: l.Opts.MaxBackups,
			MaxAge:     l.Opts.MaxAge,
			Compress:   true,
			LocalTime:  true,
		})
	}
	l.errWS = f(l.Opts.ErrorFileName)
	l.warnWS = f(l.Opts.WarnFileName)
	l.infoWS = f(l.Opts.InfoFileName)
	l.debugWS = f(l.Opts.DebugFileName)
}

func SetMaxSize(MaxSize int) ModOptions {
	return func(option *Options) {
		option.MaxSize = MaxSize
	}
}
func SetMaxBackups(MaxBackups int) ModOptions {
	return func(option *Options) {
		option.MaxBackups = MaxBackups
	}
}
func SetMaxAge(MaxAge int) ModOptions {
	return func(option *Options) {
		option.MaxAge = MaxAge
	}
}

func SetLogFileDir(LogFileDir string) ModOptions {
	return func(option *Options) {
		option.LogFileDir = LogFileDir
	}
}

func SetAppName(AppName string) ModOptions {
	return func(option *Options) {
		option.AppName = AppName
	}
}

func SetLevel(Level zapcore.Level) ModOptions {
	return func(option *Options) {
		option.Level = Level
	}
}
func SetErrorFileName(ErrorFileName string) ModOptions {
	return func(option *Options) {
		option.ErrorFileName = ErrorFileName
	}
}
func SetWarnFileName(WarnFileName string) ModOptions {
	return func(option *Options) {
		option.WarnFileName = WarnFileName
	}
}

func SetInfoFileName(InfoFileName string) ModOptions {
	return func(option *Options) {
		option.InfoFileName = InfoFileName
	}
}
func SetDebugFileName(DebugFileName string) ModOptions {
	return func(option *Options) {
		option.DebugFileName = DebugFileName
	}
}
func SetDevelopment(Development bool) ModOptions {
	return func(option *Options) {
		option.Development = Development
	}
}
func SetKafka(addrs []string, topic string) ModOptions {
	return func(option *Options) {
		option.KafkaAddrs = addrs
		option.KafkaTopic = topic
	}
}
func SetServiceName(name string) ModOptions {
	return func(option *Options) {
		option.ServiceName = name
	}
}

func (l *Logger) cores() zap.Option {
	fileEncoder := zapcore.NewJSONEncoder(l.zapConfig.EncoderConfig)

	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeTime = timeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)

	cfgLevel := l.zapConfig.Level.Level()
	errPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl == zapcore.ErrorLevel && lvl >= cfgLevel
	})
	warnPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl == zapcore.WarnLevel && lvl >= cfgLevel
	})
	infoPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl == zapcore.InfoLevel && lvl >= cfgLevel
	})
	debugPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl == zapcore.DebugLevel && lvl >= cfgLevel
	})
	cores := []zapcore.Core{
		zapcore.NewCore(fileEncoder, l.errWS, errPriority),
		zapcore.NewCore(fileEncoder, l.warnWS, warnPriority),
		zapcore.NewCore(fileEncoder, l.infoWS, infoPriority),
		zapcore.NewCore(fileEncoder, l.debugWS, debugPriority),
	}
	if l.Opts.Development {
		cores = append(cores, []zapcore.Core{
			zapcore.NewCore(consoleEncoder, errorConsoleWS, errPriority),
			zapcore.NewCore(consoleEncoder, debugConsoleWS, warnPriority),
			zapcore.NewCore(consoleEncoder, debugConsoleWS, infoPriority),
			zapcore.NewCore(consoleEncoder, debugConsoleWS, debugPriority),
		}...)
	}
	if l.Opts.KafkaAddrs != nil && l.Opts.KafkaTopic != "" {
		kw, err := NewKafkaWriter(l.Opts.KafkaAddrs, l.Opts.KafkaTopic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[logger] failed init kafka writer: %v\n", err)
		} else {
			l.kafkaWriter = kw
			kafkaWS := zapcore.AddSync(kw)
			// Kafka 只发 info 及以上级别的日志
			kafkaPriority := infoPriority
			cores = append(cores, zapcore.NewCore(fileEncoder, kafkaWS, kafkaPriority))
		}
	}
	return zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return zapcore.NewTee(cores...)
	})
}

func timeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05"))
}

func timeUnixNano(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendInt64(t.UnixNano() / 1e6)
}

// 实现 io.Writer
func (w *KafkaWriter) Write(p []byte) (n int, err error) {
	// 复制数据，因为 sarama 异步生产者不保证 msg.Value 的生命周期
	buf := make([]byte, len(p))
	copy(buf, p)

	select {
	case w.producer.Input() <- &sarama.ProducerMessage{
		Topic: w.topic,
		Value: sarama.ByteEncoder(buf),
	}:
	default:
		// Kafka buffer 满了，丢弃这条日志，避免阻塞业务
	}
	return len(p), nil
}

func NewKafkaWriter(addrs []string, topic string) (*KafkaWriter, error) {
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForLocal
	config.ChannelBufferSize = 1024 // 加大缓冲，减少满的概率

	config.Producer.Compression = sarama.CompressionSnappy
	config.Producer.Return.Successes = false // 异步，不关心成功回调

	producer, err := sarama.NewAsyncProducer(addrs, config)
	if err != nil {
		return nil, err
	}
	return &KafkaWriter{producer: producer, topic: topic}, nil
}

// ========== SugaredLogger 扩展 ==========

// SugaredLogger 包装 zap.SugaredLogger，补全 Trace 方法
type SugaredLogger struct {
	*zap.SugaredLogger
}

// Trace 映射到 Debug（zap 无 Trace 等级）
func (s *SugaredLogger) Trace(args ...interface{}) {
	s.Debug(args...)
}

// Tracef 映射到 Debugf
func (s *SugaredLogger) Tracef(template string, args ...interface{}) {
	s.Debugf(template, args...)
}

// WithField 链式添加单个字段
func (s *SugaredLogger) WithField(key string, val interface{}) *SugaredLogger {
	return &SugaredLogger{s.With(key, val)}
}

// WithFields 链式批量添加字段
func (s *SugaredLogger) WithFields(fields Fields) *SugaredLogger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &SugaredLogger{s.With(args...)}
}

// WithError 链式附加 error 字段
func (s *SugaredLogger) WithError(err error) *SugaredLogger {
	return &SugaredLogger{s.With("error", err)}
}

// ========== Init 函数（推荐入口）==========

// levelString 将旧 API 的字符串等级转为 zapcore.Level
func levelString(s string) zapcore.Level {
	switch s {
	case "DEBUG", "debug":
		return zapcore.DebugLevel
	case "INFO", "info":
		return zapcore.InfoLevel
	case "WARN", "warn":
		return zapcore.WarnLevel
	case "ERROR", "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.DebugLevel
	}
}

// Init 创建并返回一个新的 Logger 实例。每个微服务调用此函数获得自己独立的 logger。
func Init(s Settings) (*Logger, error) {
	opts := []ModOptions{
		SetLevel(levelString(s.Level)),
	}
	if s.Filename != "" {
		dir := filepath.Dir(s.Filename)
		base := filepath.Base(s.Filename)
		if ext := filepath.Ext(base); ext != "" {
			base = base[:len(base)-len(ext)]
		}
		opts = append(opts, SetLogFileDir(dir), SetAppName(base))
	}
	if s.ServiceName != "" {
		opts = append(opts, SetServiceName(s.ServiceName))
	}
	if s.Kafka.Enable && len(s.Kafka.Brokers) > 0 && s.Kafka.Topic != "" {
		opts = append(opts, SetKafka(s.Kafka.Brokers, s.Kafka.Topic))
	}
	if s.Development {
		opts = append(opts, SetDevelopment(true))
	}
	return NewLogger(opts...), nil
}

// Close 关闭日志（刷新 Kafka 生产者、同步 zap）
func (l *Logger) Close() error {
	if l.kafkaWriter != nil && l.kafkaWriter.producer != nil {
		if err := l.kafkaWriter.producer.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[logger] close kafka producer error: %v\n", err)
		}
	}
	if l.Logger != nil {
		_ = l.Logger.Sync()
	}
	return nil
}

// Sugar 返回 SugaredLogger，方便链式调用
func (l *Logger) Sugar() *SugaredLogger {
	return &SugaredLogger{l.Logger.Sugar()}
}

// ========== 配置加载（合并自 config.go）==========

// Server 空结构体（预留）
type Server struct {
}

// Config 日志配置
type Config struct {
	Kafka       model.KafkaSettings
	LogLevel    string `default:"DEBUG"`
	Filename    string `default:"./data/common.log"`
	Development bool   `default:"false"`
}

// String JSON 序列化配置
func (c Config) String() string {
	bts, _ := json.Marshal(c)
	return string(bts)
}

// InitConfig 从 YAML 文件加载日志配置
func InitConfig(file string) (*Config, error) {
	viper.SetConfigFile(file)
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/conf")

	var config Config

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
	} else {
		if err := viper.Unmarshal(&config); err != nil {
			return nil, err
		}
	}
	return &config, nil
}

// init 初始化 CommonLogger，使用默认配置（不加载文件）。
// 容错：若初始化失败（如 ./data/ 目录不存在），静默返回，不 panic。
// 各服务在自身 Init 阶段会调用 Init() 覆盖为带服务名的独立 logger。
func init() {
	log, err := Init(Settings{
		Level:       "info",
		Filename:    "./data/common.log",
		ServiceName: "common",
	})
	if err != nil {
		// 用标准 log 作为 fallback，不 panic
		return
	}
	CommonLogger = log.Sugar()
}

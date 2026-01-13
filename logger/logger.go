package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
)

// Level 日志级别类型
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

// 级别名称映射
var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

// Field 日志字段结构
type Field struct {
	Key   string
	Value any
}

// Logger 优化的日志器
type Logger struct {
	level        int64       // 使用原子操作的日志级别
	logger       *log.Logger // log.Logger本身线程安全，移除mutex
	logFile      *os.File
	writers      []io.Writer
	enableCaller bool // 控制是否获取调用栈信息（包含文件与函数名）
	callerSkip   int  // 调用栈深度
}

var (
	defaultLogger *Logger
)

// 初始化默认logger
func init() {
	defaultLogger = createLogger()
}

// createLogger 创建并配置logger实例
func createLogger() *Logger {
	logger := &Logger{
		level:        int64(INFO),
		writers:      []io.Writer{os.Stdout}, // 默认输出到控制台
		enableCaller: false,                  // 默认禁用调用栈获取（可通过LOG_ENABLE_CALLER开启）
		callerSkip:   3,                      // 默认调用栈深度
	}

	// 从环境变量设置级别
	if debug := os.Getenv("DEBUG"); debug != "" && (debug == "true" || debug == "1") {
		atomic.StoreInt64(&logger.level, int64(DEBUG))
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if level, err := ParseLevel(logLevel); err == nil {
			atomic.StoreInt64(&logger.level, int64(level))
		}
	}

	// 从环境变量控制优化特性
	if enableCaller := os.Getenv("LOG_ENABLE_CALLER"); enableCaller == "true" || enableCaller == "1" {
		logger.enableCaller = true
	} else {
		// 在调试级别时，默认开启调用者信息，便于定位（KISS）
		if lvl := os.Getenv("LOG_LEVEL"); strings.ToLower(lvl) == "debug" {
			logger.enableCaller = true
		}
		if debug := os.Getenv("DEBUG"); debug == "true" || debug == "1" {
			logger.enableCaller = true
		}
	}
	if callerSkip := os.Getenv("LOG_CALLER_SKIP"); callerSkip != "" {
		if skip, err := strconv.Atoi(callerSkip); err == nil && skip > 0 {
			logger.callerSkip = skip
		}
	}

	// 设置文件输出
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		if file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
			logger.logFile = file
			// 检查是否禁用控制台输出
			if os.Getenv("LOG_CONSOLE") == "false" {
				logger.writers = []io.Writer{file} // 只输出到文件
			} else {
				logger.writers = []io.Writer{os.Stdout, file} // 同时输出到控制台和文件
			}
		} else {
			fmt.Fprintf(os.Stderr, "无法打开日志文件 %s: %v\n", logFile, err)
		}
	}

	// 创建多写入器
	multiWriter := io.MultiWriter(logger.writers...)
	logger.logger = log.New(multiWriter, "", 0)

	return logger
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return DEBUG, nil
	case "INFO":
		return INFO, nil
	case "WARN", "WARNING":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	case "FATAL":
		return FATAL, nil
	default:
		return INFO, fmt.Errorf("unknown log level: %s", s)
	}
}

// shouldLog 检查是否应该记录指定级别的日志（优化：原子操作）
func (l *Logger) shouldLog(level Level) bool {
	return atomic.LoadInt64(&l.level) <= int64(level)
}

// LogEntry 有序日志条目结构 - 确保字段输出顺序固定
type LogEntry struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	File      string         `json:"file,omitempty"`
	Func      string         `json:"func,omitempty"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"-"` // 动态字段，单独处理
}

// log 内部日志记录方法（优化版本）
func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.shouldLog(level) {
		return
	}

	// 构建标准日志条目
	entry := &LogEntry{
		Timestamp: time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		Level:     levelNames[level],
		Message:   msg,
		Fields:    make(map[string]any),
	}

	// 按需获取调用者信息（优化：可配置）
	if l.enableCaller {
		if pc, file, line, ok := runtime.Caller(l.callerSkip); ok {
			if idx := strings.LastIndex(file, "/"); idx >= 0 {
				file = file[idx+1:]
			}
			entry.File = fmt.Sprintf("%s:%d", file, line)
			// 提取函数名，便于快速定位日志来源
			if fn := runtime.FuncForPC(pc); fn != nil {
				name := fn.Name()
				// 裁剪包路径，仅保留最后的符号名
				if dot := strings.LastIndex(name, "."); dot >= 0 && dot < len(name)-1 {
					name = name[dot+1:]
				}
				entry.Func = name
			}
		}
	}

	// 收集动态字段，过滤重复的系统字段
	for _, field := range fields {
		// 跳过重复的系统字段
		if field.Key == "level" || field.Key == "log_level" ||
			field.Key == "timestamp" || field.Key == "message" ||
			field.Key == "file" || field.Key == "log_file" ||
			field.Key == "func" {
			continue
		}
		entry.Fields[field.Key] = field.Value
	}

	// 使用自定义序列化确保字段顺序
	jsonData := l.marshalLogEntry(entry)

	// 直接输出日志 - log.Logger本身已经线程安全！
	l.logger.Println(string(jsonData))

	// Fatal级别退出程序
	if level == FATAL {
		os.Exit(1)
	}
}

// marshalLogEntry 自定义日志条目序列化，确保字段顺序（使用对象池优化）
func (l *Logger) marshalLogEntry(entry *LogEntry) []byte {
	var b strings.Builder

	// 手动构建JSON字符串确保字段顺序：timestamp > level > file > func > message > 其他字段
	b.WriteString(`{"timestamp":"`)
	b.WriteString(entry.Timestamp)
	b.WriteString(`","level":"`)
	b.WriteString(entry.Level)
	b.WriteString(`"`)

	// 添加可选字段
	if entry.File != "" {
		b.WriteString(`,"file":"`)
		b.WriteString(entry.File)
		b.WriteString(`"`)
	}
	if entry.Func != "" {
		b.WriteString(`,"func":"`)
		b.WriteString(entry.Func)
		b.WriteString(`"`)
	}

	b.WriteString(`,"message":"`)
	// 转义message中的特殊字符
	escapedMsg, _ := sonic.MarshalString(entry.Message)
	// 移除外层引号
	if len(escapedMsg) >= 2 {
		b.WriteString(escapedMsg[1 : len(escapedMsg)-1])
	}
	b.WriteString(`"`)

	// 添加动态字段（按键名排序确保一致性）
	if len(entry.Fields) > 0 {
		keys := make([]string, 0, len(entry.Fields))

		// 收集字段名
		for k := range entry.Fields {
			keys = append(keys, k)
		}
		// 使用Go标准库高效排序算法（O(n log n)）
		sort.Strings(keys)

		for _, k := range keys {
			v := entry.Fields[k]
			b.WriteString(`,"`)
			b.WriteString(k)
			b.WriteString(`":`)
			// 序列化字段值
			if fieldJSON, err := sonic.Marshal(v); err == nil {
				b.Write(fieldJSON)
			} else {
				b.WriteString(`null`)
			}
		}
	}

	b.WriteString(`}`)
	return []byte(b.String())
}

// SetLevel 设置日志级别（优化：原子操作）
func SetLevel(level Level) {
	atomic.StoreInt64(&defaultLogger.level, int64(level))
}

// 全局日志函数
func Debug(msg string, fields ...Field) {
	defaultLogger.log(DEBUG, msg, fields)
}

func Info(msg string, fields ...Field) {
	defaultLogger.log(INFO, msg, fields)
}

func Warn(msg string, fields ...Field) {
	defaultLogger.log(WARN, msg, fields)
}

func Error(msg string, fields ...Field) {
	defaultLogger.log(ERROR, msg, fields)
}

func Fatal(msg string, fields ...Field) {
	defaultLogger.log(FATAL, msg, fields)
}

// 字段构造函数
func String(key, val string) Field {
	return Field{Key: key, Value: val}
}

func Int(key string, val int) Field {
	return Field{Key: key, Value: val}
}

func Int64(key string, val int64) Field {
	return Field{Key: key, Value: val}
}

func Float64(key string, val float64) Field {
	return Field{Key: key, Value: val}
}

func Bool(key string, val bool) Field {
	return Field{Key: key, Value: val}
}

func Err(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Value: val}
}

func Any(key string, val any) Field {
	return Field{Key: key, Value: val}
}

// Reinitialize 重新初始化默认logger（用于.env文件加载后）
func Reinitialize() {
	if defaultLogger.logFile != nil {
		defaultLogger.logFile.Close()
	}
	defaultLogger = createLogger()
}

// OptimizationConfig 优化配置结构（新增）
type OptimizationConfig struct {
	EnableCaller bool `json:"enable_caller"`
	EnablePool   bool `json:"enable_pool"`
	CallerSkip   int  `json:"caller_skip"`
}

// Config 配置结构（兼容性）
type Config struct {
	Level Level
}

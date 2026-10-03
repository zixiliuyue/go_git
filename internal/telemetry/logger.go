package telemetry

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	rootLogger *slog.Logger
	loggerOnce sync.Once
	logMu      sync.RWMutex
)

// InitLogger 根据环境变量或传入参数初始化全局结构化日志记录器
// 环境变量支持:
//   GOGIT_LOG_LEVEL: debug / info / warn / error
//   GOGIT_LOG_FORMAT: json / text
//   GOGIT_LOG_FILE: 输出目标文件路径（默认为 os.Stderr）
func InitLogger() *slog.Logger {
	return RefreshLogger()
}

// RefreshLogger 重新根据当前环境变量构建并更新全局结构化日志记录器
func RefreshLogger() *slog.Logger {
	logMu.Lock()
	defer logMu.Unlock()
	rootLogger = buildLogger(
		os.Getenv("GOGIT_LOG_LEVEL"),
		os.Getenv("GOGIT_LOG_FORMAT"),
		os.Getenv("GOGIT_LOG_FILE"),
	)
	slog.SetDefault(rootLogger)
	return rootLogger
}

func buildLogger(levelStr, formatStr, filePath string) *slog.Logger {
	// 1. 日志级别解析
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// 默认级别为 Info
		level = slog.LevelInfo
	}

	// 2. 输出目标解析
	var out io.Writer = os.Stderr
	if filePath != "" {
		if f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			out = f
		}
	}

	// 3. 处理器格式选择 (JSON / Text)
	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.ToLower(strings.TrimSpace(formatStr)) == "json" {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}

	return slog.New(handler)
}

// ResetLogger 用于测试或重新配置日志器
func ResetLogger(levelStr, formatStr, filePath string) *slog.Logger {
	logMu.Lock()
	defer logMu.Unlock()
	rootLogger = buildLogger(levelStr, formatStr, filePath)
	slog.SetDefault(rootLogger)
	return rootLogger
}

// GetLogger 获取全局根结构化日志器
func GetLogger() *slog.Logger {
	logMu.RLock()
	l := rootLogger
	logMu.RUnlock()
	if l != nil {
		return l
	}
	return RefreshLogger()
}

// ForSubsystem 为指定子系统获取带标签的结构化子日志器
func ForSubsystem(name string) *slog.Logger {
	return GetLogger().With("subsystem", name)
}

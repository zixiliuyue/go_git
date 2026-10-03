package trace2

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// TargetType 表示 Trace2 目标的输出类型
type TargetType int

const (
	TargetEvent TargetType = iota // GIT_TRACE2_EVENT: 结构化 JSONL 事件流
	TargetPerf                     // GIT_TRACE2_PERF: 层次化文本性能流
	TargetNormal                   // GIT_TRACE2: 简短文本跟踪
)

// targetWriter 封装实际的输出目标（文件、管道或 Unix Domain Socket）
type targetWriter struct {
	targetType TargetType
	writer     io.WriteCloser
}

// openTarget 解析环境变量字符串并打开对应的 io.WriteCloser 输出目标
func openTarget(envVal string, tt TargetType) (*targetWriter, error) {
	val := strings.TrimSpace(envVal)
	if val == "" || val == "0" || strings.EqualFold(val, "false") || strings.EqualFold(val, "off") {
		return nil, nil
	}

	// 1. 标准输出 / 标准错误
	if val == "1" || val == "2" || strings.EqualFold(val, "true") || strings.EqualFold(val, "stderr") {
		return &targetWriter{targetType: tt, writer: nopWriteCloser{os.Stderr}}, nil
	}
	if strings.EqualFold(val, "stdout") {
		return &targetWriter{targetType: tt, writer: nopWriteCloser{os.Stdout}}, nil
	}

	// 2. Unix Domain Socket (支持 af_unix:[stream:|dgram:]/path/to/socket)
	if strings.HasPrefix(val, "af_unix:") {
		sockAddr := strings.TrimPrefix(val, "af_unix:")
		netType := "unix"
		if strings.HasPrefix(sockAddr, "stream:") {
			netType = "unix"
			sockAddr = strings.TrimPrefix(sockAddr, "stream:")
		} else if strings.HasPrefix(sockAddr, "dgram:") {
			netType = "unixgram"
			sockAddr = strings.TrimPrefix(sockAddr, "dgram:")
		}

		conn, err := net.Dial(netType, sockAddr)
		if err != nil {
			return nil, fmt.Errorf("trace2 无法连接 Unix Socket %s: %w", sockAddr, err)
		}
		return &targetWriter{targetType: tt, writer: conn}, nil
	}

	// 3. 常规文件路径（自动创建并追加写入）
	f, err := os.OpenFile(val, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("trace2 无法打开日志文件 %s: %w", val, err)
	}

	return &targetWriter{targetType: tt, writer: f}, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error {
	return nil
}

package transport

import (
	"errors"
	"fmt"
	"io"
)

const (
	SidebandData     byte = 1
	SidebandProgress byte = 2
	SidebandError    byte = 3
)

// DemuxSideband 将 sideband 多路复用数据包分流到各自的目标通道。
// channel 1 写入 packWriter，channel 2 写入 progressWriter，channel 3 转化为 Go error 返回。
func DemuxSideband(r io.Reader, packWriter io.Writer, progressWriter io.Writer) error {
	for {
		payload, pType, err := ReadPacket(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if pType == PktFlush || pType == PktResponseEnd {
			// 传输结束
			return nil
		}
		if pType == PktDelim {
			continue
		}

		if len(payload) == 0 {
			continue
		}

		band := payload[0]
		data := payload[1:]

		switch band {
		case SidebandData:
			if packWriter != nil && len(data) > 0 {
				if _, err := packWriter.Write(data); err != nil {
					return fmt.Errorf("writing pack data: %w", err)
				}
			}
		case SidebandProgress:
			if progressWriter != nil && len(data) > 0 {
				_, _ = progressWriter.Write(data)
			}
		case SidebandError:
			return fmt.Errorf("remote error: %s", string(data))
		default:
			// 非标准 sideband 格式时直接视为 pack 数据
			if packWriter != nil {
				if _, err := packWriter.Write(payload); err != nil {
					return fmt.Errorf("writing fallback pack data: %w", err)
				}
			}
		}
	}
}

// SidebandWriter 辅助在服务端写入带 sideband 包装的 pkt-line
type SidebandWriter struct {
	w       io.Writer
	channel byte
}

func NewSidebandWriter(w io.Writer, channel byte) *SidebandWriter {
	return &SidebandWriter{w: w, channel: channel}
}

func (s *SidebandWriter) Write(p []byte) (int, error) {
	const maxChunk = 65515 // 65520 - 4(pkt header) - 1(channel)
	total := 0
	for len(p) > 0 {
		chunkSize := len(p)
		if chunkSize > maxChunk {
			chunkSize = maxChunk
		}
		chunk := p[:chunkSize]
		p = p[chunkSize:]

		payload := make([]byte, 1+len(chunk))
		payload[0] = s.channel
		copy(payload[1:], chunk)

		if err := WritePacket(s.w, payload); err != nil {
			return total, err
		}
		total += len(chunk)
	}
	return total, nil
}

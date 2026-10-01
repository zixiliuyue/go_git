package transport

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// PacketType 定义 pkt-line 数据包类型
type PacketType int

const (
	PktData PacketType = iota
	PktFlush
	PktDelim
	PktResponseEnd
)

var (
	ErrPayloadTooLarge = errors.New("pkt-line payload exceeds 65516 bytes")
	FlushPktBytes      = []byte("0000")
	DelimPktBytes      = []byte("0001")
	ResponseEndBytes   = []byte("0002")
)

// ReadPacket 从流中读取下一个 pkt-line 数据包并返回有效载荷与包类型。
// 遵循 Git pkt-line 规范：前 4 字节为十六进制长度（含这 4 字节本身）。
// 特殊包：0000 为 flush-pkt，0001 为 delim-pkt，0002 为 response-end-pkt。
func ReadPacket(r io.Reader) ([]byte, PacketType, error) {
	var lenHex [4]byte
	if _, err := io.ReadFull(r, lenHex[:]); err != nil {
		return nil, PktData, err
	}

	lenBuf := make([]byte, 2)
	n, err := hex.Decode(lenBuf, lenHex[:])
	if err != nil || n != 2 {
		return nil, PktData, fmt.Errorf("invalid pkt-line length hex: %q", string(lenHex[:]))
	}

	pktLen := int(lenBuf[0])<<8 | int(lenBuf[1])

	switch pktLen {
	case 0:
		return nil, PktFlush, nil
	case 1:
		return nil, PktDelim, nil
	case 2:
		return nil, PktResponseEnd, nil
	}

	if pktLen < 4 {
		return nil, PktData, fmt.Errorf("invalid pkt-line length %d (must be 0, 1, 2 or >= 4)", pktLen)
	}

	payloadLen := pktLen - 4
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, PktData, fmt.Errorf("reading pkt-line payload of len %d: %w", payloadLen, err)
	}

	return payload, PktData, nil
}

// EncodePacket 将二进制载荷打包为 4 字节十六进制前缀的 pkt-line 字节切片
func EncodePacket(payload []byte) ([]byte, error) {
	totalLen := len(payload) + 4
	if totalLen > 65520 {
		return nil, ErrPayloadTooLarge
	}

	buf := make([]byte, totalLen)
	hex.Encode(buf[:4], []byte{byte(totalLen >> 8), byte(totalLen & 0xFF)})
	copy(buf[4:], payload)
	return buf, nil
}

// EncodePacketString 便捷函数：将字符串编码为 pkt-line
func EncodePacketString(s string) []byte {
	pkt, _ := EncodePacket([]byte(s))
	return pkt
}

// WritePacket 将载荷编码并写入目标 writer
func WritePacket(w io.Writer, payload []byte) error {
	pkt, err := EncodePacket(payload)
	if err != nil {
		return err
	}
	_, err = w.Write(pkt)
	return err
}

// WritePacketString 便捷方法：将字符串格式写为 pkt-line
func WritePacketString(w io.Writer, s string) error {
	return WritePacket(w, []byte(s))
}

// WriteFlush 写入 0000 刷新包
func WriteFlush(w io.Writer) error {
	_, err := w.Write(FlushPktBytes)
	return err
}

// WriteDelim 写入 0001 分隔包（Git 协议 v2 核心元素）
func WriteDelim(w io.Writer) error {
	_, err := w.Write(DelimPktBytes)
	return err
}

// WriteResponseEnd 写入 0002 响应结束包
func WriteResponseEnd(w io.Writer) error {
	_, err := w.Write(ResponseEndBytes)
	return err
}

// PktLineScanner 辅助扫描器：连续读取 pkt-line 载荷，遇到 Flush 或 EOF 时结束
type PktLineScanner struct {
	r   io.Reader
	buf bytes.Buffer
}

func NewPktLineScanner(r io.Reader) *PktLineScanner {
	return &PktLineScanner{r: r}
}

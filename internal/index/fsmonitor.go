package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// FSMonitorExtension 封装 Git FSMN (File System Monitor) 索引扩展
// 包含协议版本、守护进程时钟标记 (token) 与对应条目状态位图
type FSMonitorExtension struct {
	Version uint32 // 通常为 2
	Token   string // 守护进程返回的时钟或时间戳标识符
	Bitmap  []byte // ewah 位图二进制
}

// ParseFSMonitor 解析 FSMN 扩展二进制载荷
func ParseFSMonitor(data []byte) (*FSMonitorExtension, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("FSMN 扩展数据长度不足: %d", len(data))
	}
	version := binary.BigEndian.Uint32(data[0:4])
	tokenEnd := bytes.IndexByte(data[4:], 0)
	if tokenEnd < 0 {
		return &FSMonitorExtension{
			Version: version,
			Token:   string(data[4:]),
		}, nil
	}
	token := string(data[4 : 4+tokenEnd])
	bitmap := data[4+tokenEnd+1:]
	return &FSMonitorExtension{
		Version: version,
		Token:   token,
		Bitmap:  bitmap,
	}, nil
}

// EncodeFSMonitor 构建 FSMN 扩展二进制载荷（符合 Git index FSMN 规范）
func EncodeFSMonitor(version uint32, token string, numEntries int, validFlags []bool) []byte {
	var buf bytes.Buffer
	var verBuf [4]byte
	binary.BigEndian.PutUint32(verBuf[:], version)
	buf.Write(verBuf[:])
	buf.WriteString(token)
	buf.WriteByte(0) // 以 NUL 结尾

	// 构建 EWAH 位图 (兼容 Git 原生规范)
	numWords := (numEntries + 63) / 64
	if numWords == 0 {
		numWords = 1
	}
	words := make([]uint64, numWords)
	for i, v := range validFlags {
		if v && i < numEntries {
			words[i/64] |= (1 << (i % 64))
		}
	}

	// ewah 序列化：bit_size(4) + buffer_size(4) + buffer(8*N) + rlw(4)
	ewahBuf := make([]byte, 12+numWords*8)
	binary.BigEndian.PutUint32(ewahBuf[0:4], uint32(numEntries))
	binary.BigEndian.PutUint32(ewahBuf[4:8], uint32(numWords))
	for i, w := range words {
		binary.BigEndian.PutUint64(ewahBuf[8+i*8:16+i*8], w)
	}
	binary.BigEndian.PutUint32(ewahBuf[8+numWords*8:12+numWords*8], uint32(numWords))

	buf.Write(ewahBuf)
	return buf.Bytes()
}

// GetFSMonitorExtension 从 Index 中查找 FSMN 扩展
func (idx *Index) GetFSMonitorExtension() (*FSMonitorExtension, error) {
	for _, ext := range idx.Extensions {
		if ext.Signature == [4]byte{'F', 'S', 'M', 'N'} {
			return ParseFSMonitor(ext.Data)
		}
	}
	return nil, nil
}

// SetFSMonitorExtension 在 Index 中设置或更新 FSMN 扩展
func (idx *Index) SetFSMonitorExtension(fsmn *FSMonitorExtension) {
	numEntries := len(idx.Entries)
	validFlags := make([]bool, numEntries)
	for i, e := range idx.Entries {
		validFlags[i] = e.IsFSMonitorValid()
	}
	data := EncodeFSMonitor(fsmn.Version, fsmn.Token, numEntries, validFlags)

	for i, ext := range idx.Extensions {
		if ext.Signature == [4]byte{'F', 'S', 'M', 'N'} {
			idx.Extensions[i].Data = data
			return
		}
	}

	idx.Extensions = append(idx.Extensions, IndexExtension{
		Signature: [4]byte{'F', 'S', 'M', 'N'},
		Data:      data,
	})
}

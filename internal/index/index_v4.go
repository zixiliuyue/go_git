package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"os"
	"path/filepath"
)

// DecodeVarint 解码 Git Index v4 与 Packfile 所使用的 7 位带偏移量变长整数 (varint)
func DecodeVarint(buf []byte) (uint64, int, error) {
	if len(buf) == 0 {
		return 0, 0, errors.New("varint buffer 为空")
	}
	var val uint64
	idx := 0
	b := buf[idx]
	val = uint64(b & 0x7f)
	for (b & 0x80) != 0 {
		idx++
		if idx >= len(buf) {
			return 0, 0, errors.New("varint 字节流意外截断")
		}
		b = buf[idx]
		val += 1
		val = (val << 7) + uint64(b&0x7f)
	}
	return val, idx + 1, nil
}

// EncodeVarint 按照 Git 规范将无符号整数编码为 7 位带偏移量 varint
func EncodeVarint(val uint64) []byte {
	var buf []byte
	b := byte(val & 0x7f)
	val >>= 7
	for val > 0 {
		val--
		buf = append([]byte{byte((val & 0x7f) | 0x80)}, buf...)
		val >>= 7
	}
	buf = append(buf, b)
	return buf
}

// ParseIndexV4 解析 Index v4 格式（含前缀路径压缩与零 8 字节填充对齐）
func ParseIndexV4(data []byte, numEntries uint32) ([]*IndexEntry, int, error) {
	cur := data
	entries := make([]*IndexEntry, 0, numEntries)
	var prevPath string

	for i := uint32(0); i < numEntries; i++ {
		if len(cur) < 62 {
			return nil, 0, fmt.Errorf("条目 %d 数据长度不足 62 字节", i)
		}

		entry := &IndexEntry{}
		entry.CtimeSeconds = binary.BigEndian.Uint32(cur[0:4])
		entry.CtimeNanosecs = binary.BigEndian.Uint32(cur[4:8])
		entry.MtimeSeconds = binary.BigEndian.Uint32(cur[8:12])
		entry.MtimeNanosecs = binary.BigEndian.Uint32(cur[12:16])
		entry.Dev = binary.BigEndian.Uint32(cur[16:20])
		entry.Ino = binary.BigEndian.Uint32(cur[20:24])
		entry.Mode = binary.BigEndian.Uint32(cur[24:28])
		entry.UID = binary.BigEndian.Uint32(cur[28:32])
		entry.GID = binary.BigEndian.Uint32(cur[32:36])
		entry.Size = binary.BigEndian.Uint32(cur[36:40])
		copy(entry.OID[:], cur[40:60])
		entry.Flags = binary.BigEndian.Uint16(cur[60:62])

		offset := 62
		if (entry.Flags & 0x4000) != 0 {
			if len(cur) < offset+2 {
				return nil, 0, fmt.Errorf("条目 %d 缺少 extended_flags", i)
			}
			entry.ExtendedFlags = binary.BigEndian.Uint16(cur[offset : offset+2])
			offset += 2
		}

		// 解码路径前缀压缩：stripCount (从上一条目路径尾部移除的字节数)
		stripCount, vBytes, err := DecodeVarint(cur[offset:])
		if err != nil {
			return nil, 0, fmt.Errorf("条目 %d varint 路径解析失败: %w", i, err)
		}
		offset += vBytes

		// 寻找路径终止符 '\0'
		nullIdx := bytes.IndexByte(cur[offset:], 0)
		if nullIdx < 0 {
			return nil, 0, fmt.Errorf("条目 %d 路径未以 null 结尾", i)
		}
		suffix := string(cur[offset : offset+nullIdx])
		offset += nullIdx + 1 // 跳过 '\0'

		var fullPath string
		if prevPath == "" || stripCount > uint64(len(prevPath)) {
			fullPath = suffix
		} else {
			prefixLen := uint64(len(prevPath)) - stripCount
			fullPath = prevPath[:prefixLen] + suffix
		}

		entry.Path = fullPath
		prevPath = fullPath
		entries = append(entries, entry)
		cur = cur[offset:]
	}

	consumed := len(data) - len(cur)
	return entries, consumed, nil
}

// SerializeIndexV4 将条目以 Index v4 格式（前缀压缩路径）序列化
func SerializeIndexV4(entries []*IndexEntry) []byte {
	var buf bytes.Buffer
	var prevPath string

	for _, e := range entries {
		// 写入 62 字节头部
		var head [62]byte
		binary.BigEndian.PutUint32(head[0:4], e.CtimeSeconds)
		binary.BigEndian.PutUint32(head[4:8], e.CtimeNanosecs)
		binary.BigEndian.PutUint32(head[8:12], e.MtimeSeconds)
		binary.BigEndian.PutUint32(head[12:16], e.MtimeNanosecs)
		binary.BigEndian.PutUint32(head[16:20], e.Dev)
		binary.BigEndian.PutUint32(head[20:24], e.Ino)
		binary.BigEndian.PutUint32(head[24:28], e.Mode)
		binary.BigEndian.PutUint32(head[28:32], e.UID)
		binary.BigEndian.PutUint32(head[32:36], e.GID)
		binary.BigEndian.PutUint32(head[36:40], e.Size)
		copy(head[40:60], e.OID[:])
		binary.BigEndian.PutUint16(head[60:62], e.Flags)
		buf.Write(head[:])

		if (e.Flags & 0x4000) != 0 {
			var ext [2]byte
			binary.BigEndian.PutUint16(ext[:], e.ExtendedFlags)
			buf.Write(ext[:])
		}

		// 计算共同前缀
		commonLen := 0
		maxLen := len(prevPath)
		if len(e.Path) < maxLen {
			maxLen = len(e.Path)
		}
		for commonLen < maxLen && prevPath[commonLen] == e.Path[commonLen] {
			commonLen++
		}

		stripCount := uint64(len(prevPath) - commonLen)
		buf.Write(EncodeVarint(stripCount))
		buf.WriteString(e.Path[commonLen:])
		buf.WriteByte(0)

		prevPath = e.Path
	}

	return buf.Bytes()
}

// ResolveSplitIndex 解析 Git split index (link 扩展)
// 如果当前索引包含 "link" 扩展，读取其关联的 sharedindex 文件并融合条目
func ResolveSplitIndex(gitDir string, idx *Index) error {
	var linkExt *IndexExtension
	for i := range idx.Extensions {
		if string(idx.Extensions[i].Signature[:]) == "link" {
			linkExt = &idx.Extensions[i]
			break
		}
	}
	if linkExt == nil || len(linkExt.Data) < 20 {
		return nil
	}

	sharedHash := object.Hash(linkExt.Data[:20])
	sharedPath := filepath.Join(gitDir, fmt.Sprintf("sharedindex.%s", sharedHash.String()))

	sharedData, err := os.ReadFile(sharedPath)
	if err != nil {
		return fmt.Errorf("读取共享索引 %s 失败: %w", sharedPath, err)
	}

	sharedIdx, err := ParseIndex(sharedData)
	if err != nil {
		return fmt.Errorf("解析共享索引失败: %w", err)
	}

	// 将当前索引未被删除/覆盖的条目与共享索引合并
	entryMap := make(map[string]*IndexEntry)
	for _, e := range sharedIdx.Entries {
		entryMap[e.Path] = e
	}
	for _, e := range idx.Entries {
		entryMap[e.Path] = e
	}

	mergedEntries := make([]*IndexEntry, 0, len(entryMap))
	for _, e := range entryMap {
		mergedEntries = append(mergedEntries, e)
	}
	idx.Entries = mergedEntries
	idx.Sort()
	return nil
}

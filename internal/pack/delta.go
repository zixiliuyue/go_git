package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ApplyDelta 根据 Git pack 增量规范（LibXDiff 格式），将 delta 差异数据应用到 base 对象上，还原出目标对象数据。
func ApplyDelta(base []byte, delta []byte) ([]byte, error) {
	if len(delta) == 0 {
		return nil, errors.New("delta 数据为空")
	}

	reader := bytes.NewReader(delta)

	// 1. 读取基础对象长度（变长编码：每个字节低 7 位为数值，最高位为 1 表示后续仍有字节）
	baseLen, err := readVarint(reader)
	if err != nil {
		return nil, fmt.Errorf("读取 base 长度失败: %w", err)
	}
	if int(baseLen) != len(base) {
		return nil, fmt.Errorf("base 长度不匹配: 期望 %d, 实际 %d", baseLen, len(base))
	}

	// 2. 读取目标对象长度
	targetLen, err := readVarint(reader)
	if err != nil {
		return nil, fmt.Errorf("读取 target 长度失败: %w", err)
	}

	target := make([]byte, 0, targetLen)

	// 3. 循环解析操作码
	for reader.Len() > 0 {
		op, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}

		if op&0x80 != 0 {
			// COPY 指令：从 base 中切片复制
			var offset uint32
			var size uint32

			if op&0x01 != 0 {
				b, _ := reader.ReadByte()
				offset |= uint32(b)
			}
			if op&0x02 != 0 {
				b, _ := reader.ReadByte()
				offset |= uint32(b) << 8
			}
			if op&0x04 != 0 {
				b, _ := reader.ReadByte()
				offset |= uint32(b) << 16
			}
			if op&0x08 != 0 {
				b, _ := reader.ReadByte()
				offset |= uint32(b) << 24
			}

			if op&0x10 != 0 {
				b, _ := reader.ReadByte()
				size |= uint32(b)
			}
			if op&0x20 != 0 {
				b, _ := reader.ReadByte()
				size |= uint32(b) << 8
			}
			if op&0x40 != 0 {
				b, _ := reader.ReadByte()
				size |= uint32(b) << 16
			}

			if size == 0 {
				// Git 规范：若 size 位全为 0，代表长度为 0x10000 (65536)
				size = 0x10000
			}

			if int(offset+size) > len(base) {
				return nil, fmt.Errorf("COPY 范围越界: offset=%d, size=%d, baseLen=%d", offset, size, len(base))
			}

			target = append(target, base[offset:offset+size]...)
		} else if op > 0 {
			// INSERT 指令：直接从 delta 流中插入 op 个字节
			insertBuf := make([]byte, op)
			n, err := reader.Read(insertBuf)
			if err != nil || n != int(op) {
				return nil, fmt.Errorf("INSERT 数据不完整: 期望 %d, 实际 %d", op, n)
			}
			target = append(target, insertBuf...)
		} else {
			return nil, errors.New("无效的操作码 0")
		}
	}

	if len(target) != int(targetLen) {
		return nil, fmt.Errorf("生成目标长度不匹配: 期望 %d, 实际 %d", targetLen, len(target))
	}

	return target, nil
}

// CreateDelta 为两个对象生成 Git 标准格式的 delta 差分数据。
// 内部使用 16 字节滑动指纹索引 base 数据块，大幅压缩具有相似历史的对象。
func CreateDelta(base, target []byte) []byte {
	var buf bytes.Buffer

	// 1. 写入 base 与 target 长度的变长编码
	writeVarint(&buf, uint64(len(base)))
	writeVarint(&buf, uint64(len(target)))

	const blockSize = 16
	if len(base) < blockSize || len(target) < blockSize {
		// 数据过短直接全量 INSERT
		writeInsertAll(&buf, target)
		return buf.Bytes()
	}

	// 2. 为 base 建立 16 字节哈希表
	table := make(map[uint32][]int)
	for i := 0; i+blockSize <= len(base); i += blockSize {
		h := hashBlock(base[i : i+blockSize])
		table[h] = append(table[h], i)
	}

	// 3. 扫描 target
	var pendingInsert []byte
	tPos := 0

	for tPos < len(target) {
		bestOffset := -1
		bestLen := 0

		if tPos+blockSize <= len(target) {
			h := hashBlock(target[tPos : tPos+blockSize])
			if offsets, ok := table[h]; ok {
				for _, bOff := range offsets {
					// 确认初始块匹配后向前/向后尽最大可能延伸
					l := matchLength(base, bOff, target, tPos)
					if l > bestLen {
						bestLen = l
						bestOffset = bOff
					}
				}
			}
		}

		// 只有当匹配长度达到有收益的阈值时才生成 COPY
		if bestLen >= blockSize {
			// 刷新积累的 INSERT
			if len(pendingInsert) > 0 {
				writeInsertChunks(&buf, pendingInsert)
				pendingInsert = pendingInsert[:0]
			}

			// 写入 COPY 指令
			writeCopy(&buf, uint32(bestOffset), uint32(bestLen))
			tPos += bestLen
		} else {
			pendingInsert = append(pendingInsert, target[tPos])
			tPos++
		}
	}

	// 刷新末尾剩余的 INSERT
	if len(pendingInsert) > 0 {
		writeInsertChunks(&buf, pendingInsert)
	}

	return buf.Bytes()
}

func readVarint(r *bytes.Reader) (uint64, error) {
	var val uint64
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		val |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 64 {
			return 0, errors.New("varint 溢出")
		}
	}
	return val, nil
}

func writeVarint(buf *bytes.Buffer, v uint64) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v > 0 {
			b |= 0x80
			buf.WriteByte(b)
		} else {
			buf.WriteByte(b)
			break
		}
	}
}

func writeInsertChunks(buf *bytes.Buffer, data []byte) {
	for len(data) > 0 {
		chunkSize := len(data)
		if chunkSize > 127 {
			chunkSize = 127
		}
		buf.WriteByte(byte(chunkSize))
		buf.Write(data[:chunkSize])
		data = data[chunkSize:]
	}
}

func writeInsertAll(buf *bytes.Buffer, data []byte) {
	writeInsertChunks(buf, data)
}

func writeCopy(buf *bytes.Buffer, offset, size uint32) {
	var op byte = 0x80
	var args [7]byte
	argIdx := 0

	if offset&0xFF != 0 {
		op |= 0x01
		args[argIdx] = byte(offset & 0xFF)
		argIdx++
	}
	if (offset>>8)&0xFF != 0 {
		op |= 0x02
		args[argIdx] = byte((offset >> 8) & 0xFF)
		argIdx++
	}
	if (offset>>16)&0xFF != 0 {
		op |= 0x04
		args[argIdx] = byte((offset >> 16) & 0xFF)
		argIdx++
	}
	if (offset>>24)&0xFF != 0 {
		op |= 0x08
		args[argIdx] = byte((offset >> 24) & 0xFF)
		argIdx++
	}

	// 0x10000 默认不编码 size 字节
	if size != 0x10000 {
		if size&0xFF != 0 {
			op |= 0x10
			args[argIdx] = byte(size & 0xFF)
			argIdx++
		}
		if (size>>8)&0xFF != 0 {
			op |= 0x20
			args[argIdx] = byte((size >> 8) & 0xFF)
			argIdx++
		}
		if (size>>16)&0xFF != 0 {
			op |= 0x40
			args[argIdx] = byte((size >> 16) & 0xFF)
			argIdx++
		}
	}

	buf.WriteByte(op)
	buf.Write(args[:argIdx])
}

func hashBlock(b []byte) uint32 {
	return binary.BigEndian.Uint32(b[:4]) ^ binary.BigEndian.Uint32(b[4:8]) ^ binary.BigEndian.Uint32(b[8:12]) ^ binary.BigEndian.Uint32(b[12:16])
}

func matchLength(base []byte, bOff int, target []byte, tOff int) int {
	l := 0
	for bOff+l < len(base) && tOff+l < len(target) && base[bOff+l] == target[tOff+l] {
		l++
	}
	return l
}

package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	MIDXMagic = [4]byte{'M', 'I', 'D', 'X'}
)

// MIDXEntry 表示 multi-pack-index 中的单对象定位项
type MIDXEntry struct {
	OID       object.Hash
	PackIndex uint32
	Offset    uint64
}

// MultiPackIndex 代表解析后的 multi-pack-index 内存结构
type MultiPackIndex struct {
	Packs   []string
	Entries []MIDXEntry
	Fanout  [256]uint32
}

// WriteMIDX 在指定 pack 目录下扫描全部 .pack 与 .idx，生成标准 multi-pack-index 文件
func WriteMIDX(packDir string) (string, error) {
	files, err := os.ReadDir(packDir)
	if err != nil {
		return "", err
	}

	var packNames []string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "pack-") && strings.HasSuffix(f.Name(), ".pack") {
			idxName := strings.TrimSuffix(f.Name(), ".pack") + ".idx"
			if _, err := os.Stat(filepath.Join(packDir, idxName)); err == nil {
				packNames = append(packNames, idxName)
			}
		}
	}
	sort.Strings(packNames)

	if len(packNames) == 0 {
		return "", errors.New("目录下未找到任何合法的 pack/idx 文件对")
	}

	// 收集并去重所有对象
	objMap := make(map[object.Hash]MIDXEntry)
	for pIdx, pName := range packNames {
		idxPath := filepath.Join(packDir, pName)
		f, err := os.Open(idxPath)
		if err != nil {
			continue
		}
		idx, err := ParseIndexV2(f)
		_ = f.Close()
		if err != nil {
			continue
		}

		for i, oid := range idx.OIDs {
			if _, exists := objMap[oid]; !exists {
				objMap[oid] = MIDXEntry{
					OID:       oid,
					PackIndex: uint32(pIdx),
					Offset:    idx.Offsets[i],
				}
			}
		}
	}

	var entries []MIDXEntry
	for _, entry := range objMap {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].OID.String() < entries[j].OID.String()
	})

	// 构建分块 (Chunks)
	// 1. PNAM 分块：\0 结尾的 pack 名称列表
	var pnamBuf bytes.Buffer
	for _, name := range packNames {
		pnamBuf.WriteString(name)
		pnamBuf.WriteByte(0)
	}
	for pnamBuf.Len()%4 != 0 {
		pnamBuf.WriteByte(0)
	}

	// 2. OIDF 分块：256 个 uint32 fanout 累加计数值
	var fanout [256]uint32
	for _, e := range entries {
		fanout[e.OID[0]]++
	}
	for i := 1; i < 256; i++ {
		fanout[i] += fanout[i-1]
	}
	var oidfBuf bytes.Buffer
	for i := 0; i < 256; i++ {
		_ = binary.Write(&oidfBuf, binary.BigEndian, fanout[i])
	}

	// 3. OIDL 分块：所有 20 字节 OID 列表
	var oidlBuf bytes.Buffer
	for _, e := range entries {
		oidlBuf.Write(e.OID[:])
	}

	// 4. OOFF 分块：8 字节 (uint32 pack_id + uint32 offset)
	var ooffBuf bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&ooffBuf, binary.BigEndian, e.PackIndex)
		_ = binary.Write(&ooffBuf, binary.BigEndian, uint32(e.Offset))
	}

	// 组装整个 MIDX 文件
	var midxBuf bytes.Buffer
	// Header: 4 字节魔数 + 1 字节 version(1) + 1 字节 oid_version(1) + 1 字节 chunk_count(4) + 1 字节 base_midx(0) + 4 字节 pack_count
	midxBuf.Write(MIDXMagic[:])
	midxBuf.WriteByte(1) // version
	midxBuf.WriteByte(1) // SHA-1
	midxBuf.WriteByte(4) // 4 chunks: PNAM, OIDF, OIDL, OOFF
	midxBuf.WriteByte(0) // base midx
	_ = binary.Write(&midxBuf, binary.BigEndian, uint32(len(packNames))) // pack_count

	// Chunk Table: 每个 chunk 为 4 字节 ID + 8 字节 offset，末尾紧随一个全 0 哨兵 chunk 记录文件总长
	headerLen := uint64(12)
	chunkCount := uint64(4)
	tableLen := (chunkCount + 1) * 12
	currOffset := headerLen + tableLen

	chunks := []struct {
		id   [4]byte
		data []byte
	}{
		{[4]byte{'P', 'N', 'A', 'M'}, pnamBuf.Bytes()},
		{[4]byte{'O', 'I', 'D', 'F'}, oidfBuf.Bytes()},
		{[4]byte{'O', 'I', 'D', 'L'}, oidlBuf.Bytes()},
		{[4]byte{'O', 'O', 'F', 'F'}, ooffBuf.Bytes()},
	}

	for _, ch := range chunks {
		midxBuf.Write(ch.id[:])
		_ = binary.Write(&midxBuf, binary.BigEndian, currOffset)
		currOffset += uint64(len(ch.data))
	}
	// 哨兵 chunk (0, currOffset)
	midxBuf.Write([]byte{0, 0, 0, 0})
	_ = binary.Write(&midxBuf, binary.BigEndian, currOffset)

	// 写入各分块数据
	for _, ch := range chunks {
		midxBuf.Write(ch.data)
	}

	// 计算并追加末尾 20 字节 SHA-1 校验和
	chk := sha1.Sum(midxBuf.Bytes())
	midxBuf.Write(chk[:])

	midxPath := filepath.Join(packDir, "multi-pack-index")
	if err := os.WriteFile(midxPath, midxBuf.Bytes(), 0644); err != nil {
		return "", err
	}

	return midxPath, nil
}

// VerifyMIDX 校验 multi-pack-index 文件魔数、分块结构及校验和
func VerifyMIDX(midxPath string) error {
	data, err := os.ReadFile(midxPath)
	if err != nil {
		return err
	}
	if len(data) < 32 {
		return errors.New("multi-pack-index 文件过短")
	}

	if !bytes.Equal(data[:4], MIDXMagic[:]) {
		return errors.New("无效的 MIDX 魔数")
	}
	if data[4] != 1 {
		return fmt.Errorf("不支持的 MIDX 版本: %d", data[4])
	}

	// 校验 SHA-1
	contentLen := len(data) - 20
	computedSha := sha1.Sum(data[:contentLen])
	storedSha := data[contentLen:]
	if !bytes.Equal(computedSha[:], storedSha) {
		return errors.New("MIDX 校验和损坏")
	}

	return nil
}

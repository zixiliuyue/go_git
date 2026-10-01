package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"io"
	"sort"
)

var (
	IdxMagic   = [4]byte{0xFF, 't', 'O', 'c'}
	PackMagic  = [4]byte{'P', 'A', 'C', 'K'}
	ErrNotFound = errors.New("object not found in pack")
)

// PackEntry 描述 pack 中的单个对象索引元数据
type PackEntry struct {
	OID         object.Hash
	Offset      uint64
	CRC32       uint32
	Type        byte
	Size        uint64
	BaseOID     object.Hash // 用于 REF_DELTA
	BaseOffset  uint64      // 用于 OFS_DELTA
	Data        []byte      // 未解压或解压后的原始/delta 内容
}

// IndexV2 表示 Git .idx v2 格式的内存解析结构
type IndexV2 struct {
	Version      uint32
	Fanout       [256]uint32
	OIDs         []object.Hash
	CRCs         []uint32
	Offsets      []uint64
	PackChecksum object.Hash
	IdxChecksum  object.Hash
}

// ParseIndexV2 解析已有的 .idx v2 文件
func ParseIndexV2(r io.Reader) (*IndexV2, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != IdxMagic {
		return nil, errors.New("invalid idx magic header")
	}

	var version uint32
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, err
	}
	if version != 2 {
		return nil, fmt.Errorf("unsupported idx version %d, only v2 is supported", version)
	}

	idx := &IndexV2{Version: version}

	// 1. 读取 256 项 fanout 表
	if err := binary.Read(r, binary.BigEndian, &idx.Fanout); err != nil {
		return nil, fmt.Errorf("reading fanout table: %w", err)
	}

	totalObjects := int(idx.Fanout[255])
	idx.OIDs = make([]object.Hash, totalObjects)
	idx.CRCs = make([]uint32, totalObjects)
	idx.Offsets = make([]uint64, totalObjects)

	// 2. 读取 SHA 表
	for i := 0; i < totalObjects; i++ {
		if _, err := io.ReadFull(r, idx.OIDs[i][:]); err != nil {
			return nil, fmt.Errorf("reading sha table entry %d: %w", i, err)
		}
	}

	// 3. 读取 CRC32 表
	if err := binary.Read(r, binary.BigEndian, idx.CRCs); err != nil {
		return nil, fmt.Errorf("reading crc32 table: %w", err)
	}

	// 4. 读取 4 字节 offset 表
	rawOffsets := make([]uint32, totalObjects)
	if err := binary.Read(r, binary.BigEndian, rawOffsets); err != nil {
		return nil, fmt.Errorf("reading offset table: %w", err)
	}

	var largeOffsetIndices []int
	for i, off := range rawOffsets {
		if off&0x80000000 != 0 {
			largeOffsetIndices = append(largeOffsetIndices, i)
		} else {
			idx.Offsets[i] = uint64(off)
		}
	}

	// 5. 若有大于 2GB 的偏移，读取 8 字节扩展表
	if len(largeOffsetIndices) > 0 {
		for _, idxPos := range largeOffsetIndices {
			var bigOff uint64
			if err := binary.Read(r, binary.BigEndian, &bigOff); err != nil {
				return nil, fmt.Errorf("reading large offset table: %w", err)
			}
			idx.Offsets[idxPos] = bigOff
		}
	}

	// 6. 读取 Pack 校验和与 Idx 校验和
	if _, err := io.ReadFull(r, idx.PackChecksum[:]); err != nil {
		return nil, fmt.Errorf("reading pack checksum: %w", err)
	}
	if _, err := io.ReadFull(r, idx.IdxChecksum[:]); err != nil {
		return nil, fmt.Errorf("reading idx checksum: %w", err)
	}

	return idx, nil
}

// FindObject 在 idx 中查找指定 OID，通过 fanout 范围确定二分搜索区间，返回 pack 内字节偏移
func (idx *IndexV2) FindObject(oid object.Hash) (uint64, bool) {
	firstByte := oid[0]
	var low int
	if firstByte > 0 {
		low = int(idx.Fanout[firstByte-1])
	}
	high := int(idx.Fanout[firstByte])

	slice := idx.OIDs[low:high]
	target := oid.String()

	idxInSlice := sort.Search(len(slice), func(i int) bool {
		return slice[i].String() >= target
	})

	if idxInSlice < len(slice) && slice[idxInSlice] == oid {
		realIdx := low + idxInSlice
		return idx.Offsets[realIdx], true
	}

	return 0, false
}

// BuildIndexV2 根据条目列表构建标准的 .idx v2 二进制数据
func BuildIndexV2(entries []PackEntry, packChecksum object.Hash) ([]byte, error) {
	// 按照 OID 字典序升序排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].OID.String() < entries[j].OID.String()
	})

	var buf bytes.Buffer

	// 1. Magic + Version 2
	buf.Write(IdxMagic[:])
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))

	// 2. Fanout table
	var fanout [256]uint32
	for _, entry := range entries {
		fanout[entry.OID[0]]++
	}
	var cum uint32
	for i := 0; i < 256; i++ {
		cum += fanout[i]
		fanout[i] = cum
	}
	_ = binary.Write(&buf, binary.BigEndian, fanout)

	// 3. SHA table
	for _, entry := range entries {
		buf.Write(entry.OID[:])
	}

	// 4. CRC32 table
	for _, entry := range entries {
		_ = binary.Write(&buf, binary.BigEndian, entry.CRC32)
	}

	// 5. Offset table
	var largeOffsets []uint64
	for _, entry := range entries {
		if entry.Offset >= 0x80000000 {
			largeIdx := uint32(len(largeOffsets)) | 0x80000000
			_ = binary.Write(&buf, binary.BigEndian, largeIdx)
			largeOffsets = append(largeOffsets, entry.Offset)
		} else {
			_ = binary.Write(&buf, binary.BigEndian, uint32(entry.Offset))
		}
	}

	// 6. Large offsets table
	for _, bigOff := range largeOffsets {
		_ = binary.Write(&buf, binary.BigEndian, bigOff)
	}

	// 7. Pack Checksum (20B)
	buf.Write(packChecksum[:])

	// 8. Idx Checksum (20B) - 对前面所有写入字节计算 SHA-1
	idxChecksum := sha1.Sum(buf.Bytes())
	buf.Write(idxChecksum[:])

	return buf.Bytes(), nil
}

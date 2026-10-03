package pack

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// Pack 对象类型常量（与 Git 规范完全对齐）
const (
	TypeCommit   byte = 1
	TypeTree     byte = 2
	TypeBlob     byte = 3
	TypeTag      byte = 4
	TypeOFSDelta byte = 6
	TypeREFDelta byte = 7
)

// ObjectTypeFromPackType 将 pack 内部类型码转换为系统 ObjectType
func ObjectTypeFromPackType(t byte) (object.ObjectType, error) {
	switch t {
	case TypeCommit:
		return object.TypeCommit, nil
	case TypeTree:
		return object.TypeTree, nil
	case TypeBlob:
		return object.TypeBlob, nil
	case TypeTag:
		return object.TypeTag, nil
	default:
		return "", fmt.Errorf("unknown pack object type %d", t)
	}
}

// PackTypeFromObjectType 将系统 ObjectType 转换为 pack 类型码
func PackTypeFromObjectType(ot object.ObjectType) byte {
	switch ot {
	case object.TypeCommit:
		return TypeCommit
	case object.TypeTree:
		return TypeTree
	case object.TypeBlob:
		return TypeBlob
	case object.TypeTag:
		return TypeTag
	default:
		return TypeBlob
	}
}

// ResolvedObject 描述已完全解析并计算出哈希的 Pack 对象
type ResolvedObject struct {
	OID     object.Hash
	Type    object.ObjectType
	Content []byte
	Offset  uint64
	CRC32   uint32
}

// ReadPack 解析整个 pack 二进制流，解析所有基础对象与 OFS_DELTA / REF_DELTA 增量对象，并返回条目列表与 pack 校验和。
func ReadPack(rawReader io.Reader) ([]ResolvedObject, object.Hash, error) {
	// 确保具有 ByteReader 接口能力
	var br io.ByteReader
	var r io.Reader
	if b, ok := rawReader.(io.ByteReader); ok {
		br = b
		r = rawReader
	} else {
		bufr := bufio.NewReader(rawReader)
		br = bufr
		r = bufr
	}

	// 追踪全包哈希的 byte reader
	hasher := sha1.New()
	tbr := &teeByteReader{r: r, br: br, w: hasher}

	// 1. 读取头部 12 字节
	var magic [4]byte
	if _, err := io.ReadFull(tbr, magic[:]); err != nil {
		return nil, object.ZeroHash, err
	}
	if magic != PackMagic {
		return nil, object.ZeroHash, errors.New("invalid pack magic")
	}

	var version, count uint32
	if err := binary.Read(tbr, binary.BigEndian, &version); err != nil {
		return nil, object.ZeroHash, err
	}
	if version != 2 {
		return nil, object.ZeroHash, fmt.Errorf("unsupported pack version %d", version)
	}
	if err := binary.Read(tbr, binary.BigEndian, &count); err != nil {
		return nil, object.ZeroHash, err
	}
	if count > 10_000_000 {
		return nil, object.ZeroHash, fmt.Errorf("pack object count %d exceeds safety limit", count)
	}

	type rawEntry struct {
		offset     uint64
		crc32      uint32
		typeCode   byte
		rawContent []byte
		baseOffset uint64
		baseOID    object.Hash
		isResolved bool
		resolving  bool // 防止 Delta 恶意构造循环依赖形成死循环
		resType    object.ObjectType
		resContent []byte
		finalOID   object.Hash
	}

	entries := make([]*rawEntry, count)
	byOffset := make(map[uint64]*rawEntry, count)
	currentOffset := uint64(12) // 4B magic + 4B ver + 4B count

	for i := uint32(0); i < count; i++ {
		entryStart := currentOffset
		crcHasher := crc32.NewIEEE()
		tracker := &entryTracker{r: tbr, br: tbr, crc: crcHasher}

		// 读取 entry 头部（类型 + 解压尺寸）
		typeCode, _, err := readEntryHeader(tracker)
		if err != nil {
			return nil, object.ZeroHash, fmt.Errorf("reading entry %d header: %w", i, err)
		}

		var baseOff uint64
		var baseOID object.Hash

		if typeCode == TypeOFSDelta {
			// 读取负偏移量变长编码
			negOffset, err := readOFSDeltaOffset(tracker)
			if err != nil {
				return nil, object.ZeroHash, fmt.Errorf("reading ofs_delta offset: %w", err)
			}
			baseOff = entryStart - negOffset
		} else if typeCode == TypeREFDelta {
			// 读取 20 字节 base SHA-1
			if _, err := io.ReadFull(tracker, baseOID[:]); err != nil {
				return nil, object.ZeroHash, fmt.Errorf("reading ref_delta base oid: %w", err)
			}
		}

		// 解压缩 zlib 数据段：因为 tracker 实现了 io.ByteReader，zlib 内部不会使用多余的 bufio 预读！
		zr, err := zlib.NewReader(tracker)
		if err != nil {
			return nil, object.ZeroHash, fmt.Errorf("init zlib reader at entry %d: %w", i, err)
		}
		var contentBuf bytes.Buffer
		if _, err := io.Copy(&contentBuf, zr); err != nil {
			zr.Close()
			return nil, object.ZeroHash, fmt.Errorf("decompressing entry %d: %w", i, err)
		}
		zr.Close()

		entryCRC := crcHasher.Sum32()
		currentOffset += uint64(tracker.count)

		re := &rawEntry{
			offset:     entryStart,
			crc32:      entryCRC,
			typeCode:   typeCode,
			rawContent: contentBuf.Bytes(),
			baseOffset: baseOff,
			baseOID:    baseOID,
		}
		entries[i] = re
		byOffset[entryStart] = re
	}

	// 2. 读取 Pack 校验和（尾部 20 字节）
	// 注意：校验和本身不计入已计算的 hasher，所以读取 trailer 前，hasher 中恰好是所有前置字节的哈希
	expectedChecksum := object.Hash(hasher.Sum(nil))
	var packTrailer object.Hash
	if _, err := io.ReadFull(r, packTrailer[:]); err != nil {
		return nil, object.ZeroHash, fmt.Errorf("reading pack trailer: %w", err)
	}

	if expectedChecksum != packTrailer {
		return nil, object.ZeroHash, fmt.Errorf("pack checksum mismatch: expected %s, got %s", expectedChecksum.String(), packTrailer.String())
	}

	// 3. 递归/循环解析所有 Delta 对象
	byOID := make(map[object.Hash]*rawEntry, count)

	const maxDeltaDepth = 50
	var resolve func(re *rawEntry, depth int) error
	resolve = func(re *rawEntry, depth int) error {
		if re.isResolved {
			return nil
		}
		if re.resolving {
			return fmt.Errorf("detected circular delta dependency at offset %d", re.offset)
		}
		if depth > maxDeltaDepth {
			return fmt.Errorf("delta chain exceeds maximum depth %d at offset %d", maxDeltaDepth, re.offset)
		}

		re.resolving = true
		defer func() { re.resolving = false }()

		if re.typeCode != TypeOFSDelta && re.typeCode != TypeREFDelta {
			// 基础普通对象
			ot, err := ObjectTypeFromPackType(re.typeCode)
			if err != nil {
				return err
			}
			re.resType = ot
			re.resContent = re.rawContent
			re.finalOID = object.HashObject(ot, re.resContent)
			re.isResolved = true
			byOID[re.finalOID] = re
			return nil
		}

		var baseType object.ObjectType
		var baseContent []byte

		if re.typeCode == TypeOFSDelta {
			baseEntry, ok := byOffset[re.baseOffset]
			if !ok {
				return fmt.Errorf("ofs_delta base at %d not found in pack", re.baseOffset)
			}
			if err := resolve(baseEntry, depth+1); err != nil {
				return err
			}
			baseType = baseEntry.resType
			baseContent = baseEntry.resContent
		} else {
			// REF_DELTA
			baseEntry, ok := byOID[re.baseOID]
			if !ok {
				return fmt.Errorf("ref_delta base %s not found in pack", re.baseOID.String())
			}
			if err := resolve(baseEntry, depth+1); err != nil {
				return err
			}
			baseType = baseEntry.resType
			baseContent = baseEntry.resContent
		}

		applied, err := ApplyDelta(baseContent, re.rawContent)
		if err != nil {
			return fmt.Errorf("applying delta at offset %d: %w", re.offset, err)
		}

		re.resType = baseType
		re.resContent = applied
		re.finalOID = object.HashObject(baseType, applied)
		re.isResolved = true
		byOID[re.finalOID] = re
		return nil
	}

	resolvedList := make([]ResolvedObject, len(entries))
	for i, re := range entries {
		if err := resolve(re, 0); err != nil {
			return nil, object.ZeroHash, err
		}
		resolvedList[i] = ResolvedObject{
			OID:     re.finalOID,
			Type:    re.resType,
			Content: re.resContent,
			Offset:  re.offset,
			CRC32:   re.crc32,
		}
	}

	return resolvedList, packTrailer, nil
}

// SavePackAndIndex 将 pack 数据流写入 .git/objects/pack/ 目录，并同时生成匹配的 .idx 文件
func SavePackAndIndex(objectsDir string, packData []byte) (object.Hash, error) {
	packDir := filepath.Join(objectsDir, "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return object.ZeroHash, err
	}

	reader := bytes.NewReader(packData)
	resolved, packChecksum, err := ReadPack(reader)
	if err != nil {
		return object.ZeroHash, fmt.Errorf("parsing pack data: %w", err)
	}

	packEntries := make([]PackEntry, len(resolved))
	for i, r := range resolved {
		packEntries[i] = PackEntry{
			OID:    r.OID,
			Offset: r.Offset,
			CRC32:  r.CRC32,
		}
	}

	idxBytes, err := BuildIndexV2(packEntries, packChecksum)
	if err != nil {
		return object.ZeroHash, fmt.Errorf("building index v2: %w", err)
	}

	namePrefix := fmt.Sprintf("pack-%s", packChecksum.String())
	packFilePath := filepath.Join(packDir, namePrefix+".pack")
	idxFilePath := filepath.Join(packDir, namePrefix+".idx")

	if err := os.WriteFile(packFilePath, packData, 0644); err != nil {
		return object.ZeroHash, err
	}
	if err := os.WriteFile(idxFilePath, idxBytes, 0644); err != nil {
		_ = os.Remove(packFilePath)
		return object.ZeroHash, err
	}

	return packChecksum, nil
}

type teeByteReader struct {
	r  io.Reader
	br io.ByteReader
	w  io.Writer
}

func (t *teeByteReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.w.Write(p[:n])
	}
	return n, err
}

func (t *teeByteReader) ReadByte() (byte, error) {
	b, err := t.br.ReadByte()
	if err == nil {
		t.w.Write([]byte{b})
	}
	return b, err
}

type entryTracker struct {
	r     io.Reader
	br    io.ByteReader
	crc   hash.Hash32
	count int
}

func (et *entryTracker) Read(p []byte) (int, error) {
	n, err := et.r.Read(p)
	if n > 0 {
		if et.crc != nil {
			et.crc.Write(p[:n])
		}
		et.count += n
	}
	return n, err
}

func (et *entryTracker) ReadByte() (byte, error) {
	b, err := et.br.ReadByte()
	if err == nil {
		if et.crc != nil {
			et.crc.Write([]byte{b})
		}
		et.count++
	}
	return b, err
}

func readEntryHeader(r io.Reader) (byte, uint64, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, 0, err
	}

	typeCode := (b[0] >> 4) & 0x07
	size := uint64(b[0] & 0x0F)
	shift := uint(4)

	if b[0]&0x80 == 0 {
		return typeCode, size, nil
	}

	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, 0, err
		}
		size |= uint64(b[0]&0x7F) << shift
		shift += 7
		if b[0]&0x80 == 0 {
			break
		}
		if shift >= 64 {
			return 0, 0, errors.New("entry size varint overflow")
		}
	}

	return typeCode, size, nil
}

func readOFSDeltaOffset(r io.Reader) (uint64, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	offset := uint64(b[0] & 0x7F)
	for b[0]&0x80 != 0 {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		offset = ((offset + 1) << 7) | uint64(b[0]&0x7F)
	}
	return offset, nil
}

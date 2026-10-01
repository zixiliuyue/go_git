package pack

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"gogit/internal/object"
	"hash/crc32"
	"sort"
)

// PackableObject 供 PackWriter 打包的对象结构
type PackableObject struct {
	OID     object.Hash
	Type    object.ObjectType
	Content []byte
}

// BuildPack 将一组对象打包为标准 Git packfile (v2) 与对应的 .idx (v2) 数据
func BuildPack(objects []PackableObject) (packBytes []byte, idxBytes []byte, packChecksum object.Hash, err error) {
	// 1. 对象排序策略：按 Type 优先级分组（commit -> tree -> blob -> tag），同类型按尺寸相近排序以利于压缩
	sort.Slice(objects, func(i, j int) bool {
		tOrder := func(ot object.ObjectType) int {
			switch ot {
			case object.TypeCommit:
				return 1
			case object.TypeTree:
				return 2
			case object.TypeBlob:
				return 3
			case object.TypeTag:
				return 4
			default:
				return 5
			}
		}
		if tOrder(objects[i].Type) != tOrder(objects[j].Type) {
			return tOrder(objects[i].Type) < tOrder(objects[j].Type)
		}
		if len(objects[i].Content) != len(objects[j].Content) {
			return len(objects[i].Content) < len(objects[j].Content)
		}
		return objects[i].OID.String() < objects[j].OID.String()
	})

	type plannedEntry struct {
		isDelta    bool
		baseOID    object.Hash
		deltaData  []byte
		origObj    PackableObject
	}

	planned := make([]plannedEntry, len(objects))

	// 2. 滑动窗口 Delta 压缩尝试（窗口大小 8）
	windowSize := 8
	for i := 0; i < len(objects); i++ {
		cur := objects[i]
		bestSaving := 0
		bestBaseOID := object.ZeroHash
		var bestDelta []byte

		// 仅对超过 64 字节的 blob 与 tree 尝试增量
		if len(cur.Content) > 64 && (cur.Type == object.TypeBlob || cur.Type == object.TypeTree) {
			startW := i - windowSize
			if startW < 0 {
				startW = 0
			}
			for w := startW; w < i; w++ {
				baseCandidate := objects[w]
				if baseCandidate.Type != cur.Type {
					continue
				}
				delta := CreateDelta(baseCandidate.Content, cur.Content)
				// 增量大小需明显小于原内容（节省超过 25%）才采纳
				saving := len(cur.Content) - len(delta)
				if saving > len(cur.Content)/4 && saving > bestSaving {
					bestSaving = saving
					bestBaseOID = baseCandidate.OID
					bestDelta = delta
				}
			}
		}

		if bestSaving > 0 {
			planned[i] = plannedEntry{
				isDelta:   true,
				baseOID:   bestBaseOID,
				deltaData: bestDelta,
				origObj:   cur,
			}
		} else {
			planned[i] = plannedEntry{
				isDelta: false,
				origObj: cur,
			}
		}
	}

	// 3. 构建 packfile 二进制流
	var packBuf bytes.Buffer

	// 头部 12 字节
	packBuf.Write(PackMagic[:])
	_ = binary.Write(&packBuf, binary.BigEndian, uint32(2))
	_ = binary.Write(&packBuf, binary.BigEndian, uint32(len(planned)))

	packEntries := make([]PackEntry, len(planned))

	for i, p := range planned {
		entryOffset := uint64(packBuf.Len())
		crcHasher := crc32.NewIEEE()

		var entryData []byte
		var typeCode byte
		var uncompressedSize uint64

		if p.isDelta {
			typeCode = TypeREFDelta
			entryData = p.deltaData
			uncompressedSize = uint64(len(p.deltaData))
		} else {
			typeCode = PackTypeFromObjectType(p.origObj.Type)
			entryData = p.origObj.Content
			uncompressedSize = uint64(len(p.origObj.Content))
		}

		// 写入 Entry 变长头部
		headerBytes := encodeEntryHeader(typeCode, uncompressedSize)
		packBuf.Write(headerBytes)
		crcHasher.Write(headerBytes)

		// 若为 REF_DELTA，写入 20 字节 Base OID
		if p.isDelta {
			packBuf.Write(p.baseOID[:])
			crcHasher.Write(p.baseOID[:])
		}

		// zlib 压缩写入对象数据
		var zlibBuf bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&zlibBuf, zlib.DefaultCompression)
		_, _ = zw.Write(entryData)
		_ = zw.Close()

		compressedBytes := zlibBuf.Bytes()
		packBuf.Write(compressedBytes)
		crcHasher.Write(compressedBytes)

		packEntries[i] = PackEntry{
			OID:    p.origObj.OID,
			Offset: entryOffset,
			CRC32:  crcHasher.Sum32(),
		}
	}

	// 4. 计算并追加 pack trailer SHA-1 (20B)
	checksum := sha1.Sum(packBuf.Bytes())
	packChecksum = object.Hash(checksum)
	packBuf.Write(checksum[:])

	packBytes = packBuf.Bytes()

	// 5. 构建配套的 .idx v2
	idxBytes, err = BuildIndexV2(packEntries, packChecksum)
	if err != nil {
		return nil, nil, object.ZeroHash, fmt.Errorf("building index v2: %w", err)
	}

	return packBytes, idxBytes, packChecksum, nil
}

func encodeEntryHeader(typeCode byte, size uint64) []byte {
	var buf []byte
	b := (typeCode & 0x07) << 4
	b |= byte(size & 0x0F)
	size >>= 4

	if size > 0 {
		b |= 0x80
		buf = append(buf, b)
		for {
			nextByte := byte(size & 0x7F)
			size >>= 7
			if size > 0 {
				nextByte |= 0x80
				buf = append(buf, nextByte)
			} else {
				buf = append(buf, nextByte)
				break
			}
		}
	} else {
		buf = append(buf, b)
	}
	return buf
}

package pack

import (
	"crypto/sha1"
	"fmt"
	"gogit/internal/object"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ObjectVerifyStat 记录单个对象校验通过后的统计信息
type ObjectVerifyStat struct {
	OID        object.Hash
	Type       object.ObjectType
	Size       uint64
	PackSize   uint64
	Offset     uint64
	Depth      int
	BaseOID    object.Hash
	IsDelta    bool
}

// VerifyResult 包含整个 packfile 校验的完整结果
type VerifyResult struct {
	PackPath     string
	IdxPath      string
	TotalObjects int
	PackChecksum object.Hash
	IdxChecksum  object.Hash
	Objects      []ObjectVerifyStat
}

// VerifyPackFile 完整校验给定的 .pack 文件或 .idx 文件的完整性与所有对象哈希/CRC
func VerifyPackFile(packOrIdxPath string, verbose bool) (*VerifyResult, error) {
	var packPath, idxPath string
	if strings.HasSuffix(packOrIdxPath, ".idx") {
		idxPath = packOrIdxPath
		packPath = strings.TrimSuffix(packOrIdxPath, ".idx") + ".pack"
	} else if strings.HasSuffix(packOrIdxPath, ".pack") {
		packPath = packOrIdxPath
		idxPath = strings.TrimSuffix(packOrIdxPath, ".pack") + ".idx"
	} else {
		packPath = packOrIdxPath + ".pack"
		idxPath = packOrIdxPath + ".idx"
	}

	// 1. 读取并校验 idx 文件
	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, fmt.Errorf("读取 idx 文件失败: %w", err)
	}
	if len(idxData) < 40 {
		return nil, fmt.Errorf("idx 文件过短: %d 字节", len(idxData))
	}

	// 校验 idx 自身 SHA-1
	computedIdxSha := sha1.Sum(idxData[:len(idxData)-20])
	storedIdxSha := idxData[len(idxData)-20:]
	if string(computedIdxSha[:]) != string(storedIdxSha) {
		return nil, fmt.Errorf("idx 文件校验和损坏: 计算值 %x, 文件记录 %x", computedIdxSha, storedIdxSha)
	}

	// 解析 idx
	idx, err := ParseIndexV2(strings.NewReader(string(idxData)))
	if err != nil {
		return nil, fmt.Errorf("解析 idx 失败: %w", err)
	}

	// 2. 读取并校验 pack 文件
	packData, err := os.ReadFile(packPath)
	if err != nil {
		return nil, fmt.Errorf("读取 pack 文件失败: %w", err)
	}
	if len(packData) < 32 {
		return nil, fmt.Errorf("pack 文件过短: %d 字节", len(packData))
	}

	// 校验 pack 自身 SHA-1
	computedPackSha := sha1.Sum(packData[:len(packData)-20])
	storedPackSha := packData[len(packData)-20:]
	if string(computedPackSha[:]) != string(storedPackSha) {
		return nil, fmt.Errorf("pack 文件校验和损坏: 计算值 %x, 文件记录 %x", computedPackSha, storedPackSha)
	}

	// 校验 idx 中记录的 pack checksum 是否一致
	if idx.PackChecksum != object.Hash(computedPackSha) {
		return nil, fmt.Errorf("idx 记录的 pack 哈希与实际 pack 文件不匹配")
	}

	// 3. 打开 Packfile 解包并验证全部对象
	packFile, err := os.Open(packPath)
	if err != nil {
		return nil, fmt.Errorf("打开 packfile 失败: %w", err)
	}
	defer packFile.Close()

	resolvedObjs, _, err := ReadPack(packFile)
	if err != nil {
		return nil, fmt.Errorf("解包 packfile 失败: %w", err)
	}

	res := &VerifyResult{
		PackPath:     packPath,
		IdxPath:      idxPath,
		TotalObjects: len(idx.OIDs),
		PackChecksum: idx.PackChecksum,
		IdxChecksum:  object.Hash(computedIdxSha),
	}

	// 按照 offset 升序排序以计算每个对象在 pack 中的物理占用大小
	type objMeta struct {
		oid    object.Hash
		offset uint64
		crc    uint32
	}
	metas := make([]objMeta, len(idx.OIDs))
	for i := 0; i < len(idx.OIDs); i++ {
		metas[i] = objMeta{
			oid:    idx.OIDs[i],
			offset: idx.Offsets[i],
			crc:    idx.CRCs[i],
		}
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].offset < metas[j].offset
	})

	packEndOffset := uint64(len(packData) - 20)
	offsetToPackSize := make(map[uint64]uint64)

	for i := 0; i < len(metas); i++ {
		cur := metas[i]
		var nextOffset uint64
		if i+1 < len(metas) {
			nextOffset = metas[i+1].offset
		} else {
			nextOffset = packEndOffset
		}

		if cur.offset >= uint64(len(packData)) || nextOffset > uint64(len(packData)) || cur.offset >= nextOffset {
			return nil, fmt.Errorf("对象 %s offset 越界或损坏: offset=%d, next=%d", cur.oid.String(), cur.offset, nextOffset)
		}

		// 校验 CRC32
		actualCRC := crc32.ChecksumIEEE(packData[cur.offset:nextOffset])
		if actualCRC != cur.crc {
			return nil, fmt.Errorf("对象 %s CRC32 校验失败: 预期 %08x, 实际 %08x", cur.oid.String(), cur.crc, actualCRC)
		}

		offsetToPackSize[cur.offset] = nextOffset - cur.offset
	}

	// 校验已解包的对象哈希
	for _, robj := range resolvedObjs {
		computedOID := object.HashObject(robj.Type, robj.Content)
		if computedOID != robj.OID {
			return nil, fmt.Errorf("对象 %s 解码后内容哈希不匹配: 计算值 %s", robj.OID.String(), computedOID.String())
		}

		stat := ObjectVerifyStat{
			OID:      robj.OID,
			Type:     robj.Type,
			Size:     uint64(len(robj.Content)),
			PackSize: offsetToPackSize[robj.Offset],
			Offset:   robj.Offset,
		}
		res.Objects = append(res.Objects, stat)
	}

	return res, nil
}

// FormatVerboseStat 格式化输出为与 git verify-pack -v 完全兼容的文本行
func FormatVerboseStat(w io.Writer, res *VerifyResult) {
	for _, obj := range res.Objects {
		fmt.Fprintf(w, "%s %s %d %d %d\n", obj.OID.String(), string(obj.Type), obj.Size, obj.PackSize, obj.Offset)
	}
	fmt.Fprintf(w, "%s: ok\n", filepath.Base(res.PackPath))
}

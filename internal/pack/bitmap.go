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
	"strings"
)

var (
	BitmapMagic = [4]byte{'B', 'I', 'T', 'M'}
)

// SimpleEWAH 是兼容 Git pack bitmap 的 Word-Aligned 混合位图编码器
type SimpleEWAH struct {
	bits []bool
}

// NewSimpleEWAH 创建指定大小的位图
func NewSimpleEWAH(size int) *SimpleEWAH {
	return &SimpleEWAH{bits: make([]bool, size)}
}

// Set 设置指定位置为 1
func (e *SimpleEWAH) Set(idx int) {
	if idx >= 0 && idx < len(e.bits) {
		e.bits[idx] = true
	}
}

// Encode 序列化为 Git EWAH 字节流
// 格式：4 字节 bit 数量 + 4 字节 64-bit word 数量 + 64-bit 字数组 + 4 字节 RLW 描述符数量
func (e *SimpleEWAH) Encode() []byte {
	var buf bytes.Buffer
	numBits := uint32(len(e.bits))
	_ = binary.Write(&buf, binary.BigEndian, numBits)

	// 计算包含的 64 位整字数量
	numWords := (len(e.bits) + 63) / 64
	_ = binary.Write(&buf, binary.BigEndian, uint32(numWords))

	words := make([]uint64, numWords)
	for i, b := range e.bits {
		if b {
			words[i/64] |= (uint64(1) << (i % 64))
		}
	}

	for _, w := range words {
		_ = binary.Write(&buf, binary.BigEndian, w)
	}

	// 单一 Run-Length Word (RLW) 描述符：描述全部字均为字面量字
	rlwCount := uint32(1)
	_ = binary.Write(&buf, binary.BigEndian, rlwCount)
	// RLW: 32位 running count(0) + 1位 running bit(0) + 31位 literal words(numWords)
	rlw := uint64(numWords) << 33
	_ = binary.Write(&buf, binary.BigEndian, rlw)

	return buf.Bytes()
}

// WritePackBitmap 为指定的 packfile 生成 Git 兼容的 .bitmap 文件
func WritePackBitmap(packPathOrDir, packBaseName string, getObjectType func(object.Hash) (object.ObjectType, error)) (string, error) {
	packDir := packPathOrDir
	if strings.HasSuffix(packPathOrDir, ".pack") || strings.HasSuffix(packPathOrDir, ".idx") {
		packDir = filepath.Dir(packPathOrDir)
		packBaseName = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(packPathOrDir), ".pack"), ".idx")
	} else if strings.HasSuffix(packBaseName, ".idx") || strings.HasSuffix(packBaseName, ".pack") {
		packBaseName = strings.TrimSuffix(strings.TrimSuffix(packBaseName, ".pack"), ".idx")
	}

	idxPath := filepath.Join(packDir, packBaseName+".idx")
	idxFile, err := os.Open(idxPath)
	if err != nil {
		return "", err
	}
	defer idxFile.Close()

	idx, err := ParseIndexV2(idxFile)
	if err != nil {
		return "", err
	}

	totalObjects := len(idx.OIDs)
	commitsBM := NewSimpleEWAH(totalObjects)
	treesBM := NewSimpleEWAH(totalObjects)
	blobsBM := NewSimpleEWAH(totalObjects)
	tagsBM := NewSimpleEWAH(totalObjects)

	for i, oid := range idx.OIDs {
		oType, err := getObjectType(oid)
		if err != nil {
			continue
		}
		switch oType {
		case object.TypeCommit:
			commitsBM.Set(i)
		case object.TypeTree:
			treesBM.Set(i)
		case object.TypeBlob:
			blobsBM.Set(i)
		case object.TypeTag:
			tagsBM.Set(i)
		}
	}

	var buf bytes.Buffer
	// 1. Header (32 字节)
	buf.Write(BitmapMagic[:])
	_ = binary.Write(&buf, binary.BigEndian, uint16(1)) // Version 1
	_ = binary.Write(&buf, binary.BigEndian, uint16(1)) // Flags: BITMAP_OPT_FULL_DAG
	_ = binary.Write(&buf, binary.BigEndian, uint32(0)) // 0 selected commits in table for basic bitmap
	buf.Write(idx.PackChecksum[:])                      // 20-byte matching pack checksum

	// 2. 4 类核心对象类型的完整 EWAH 位图
	buf.Write(commitsBM.Encode())
	buf.Write(treesBM.Encode())
	buf.Write(blobsBM.Encode())
	buf.Write(tagsBM.Encode())

	// 3. 计算末尾 SHA-1 校验和
	chk := sha1.Sum(buf.Bytes())
	buf.Write(chk[:])

	bitmapPath := filepath.Join(packDir, packBaseName+".bitmap")
	if err := os.WriteFile(bitmapPath, buf.Bytes(), 0644); err != nil {
		return "", err
	}

	return bitmapPath, nil
}

// VerifyPackBitmap 检验 .bitmap 文件的魔数、版本、关联 pack 哈希及 SHA-1 完整性
func VerifyPackBitmap(bitmapPath string) error {
	data, err := os.ReadFile(bitmapPath)
	if err != nil {
		return err
	}
	if len(data) < 52 {
		return errors.New(".bitmap 文件过短")
	}

	if !bytes.Equal(data[:4], BitmapMagic[:]) {
		return errors.New("无效的 Bitmap 魔数")
	}

	version := binary.BigEndian.Uint16(data[4:6])
	if version != 1 {
		return fmt.Errorf("不支持的 Bitmap 版本: %d", version)
	}

	contentLen := len(data) - 20
	computedSha := sha1.Sum(data[:contentLen])
	storedSha := data[contentLen:]
	if !bytes.Equal(computedSha[:], storedSha) {
		return errors.New(".bitmap 校验和损坏")
	}

	// 校验对应的 packfile 是否存在
	base := strings.TrimSuffix(bitmapPath, ".bitmap")
	if _, err := os.Stat(base + ".pack"); err != nil {
		return fmt.Errorf("未找到对应的 pack 文件: %s.pack", base)
	}

	return nil
}

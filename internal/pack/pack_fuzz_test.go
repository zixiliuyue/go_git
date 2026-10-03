package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"testing"
)

// FuzzPackDecoder 对 Git Packfile 及 Delta 差异应用解码器进行模糊测试
// 重点验证面对 OFS_DELTA / REF_DELTA 循环依赖、解压炸弹、畸变头部时具有系统韧性
func FuzzPackDecoder(f *testing.F) {
	// 1. 构造一个合法的空 packfile 作为基础种子
	var emptyPack bytes.Buffer
	emptyPack.WriteString("PACK")
	_ = binary.Write(&emptyPack, binary.BigEndian, uint32(2)) // version 2
	_ = binary.Write(&emptyPack, binary.BigEndian, uint32(0)) // 0 objects
	checksum := sha1.Sum(emptyPack.Bytes())
	emptyPack.Write(checksum[:])

	f.Add(emptyPack.Bytes())
	f.Add([]byte("PACK\x00\x00\x00\x02\xff\xff\xff\xff")) // 恶意超大对象计数
	f.Add([]byte("PACK\x00\x00\x00\x03\x00\x00\x00\x01")) // 不支持的 version 3
	f.Add([]byte("NOTAPACK"))                             // 错误魔数
	f.Add([]byte(""))                                     // 空数据

	// 2. Fuzz Packfile 解析
	f.Fuzz(func(t *testing.T, data []byte) {
		// 验证 ReadPack 处理非可信数据时不 panic、不死循环、不 OOM
		r := bytes.NewReader(data)
		objs, _, err := ReadPack(r)
		if err == nil {
			_ = objs
		}

		// 验证 ApplyDelta 处理畸变 Delta 数据时安全拦截解压炸弹与越界
		base := []byte("The quick brown fox jumps over the lazy dog\n")
		target, err := ApplyDelta(base, data)
		if err == nil {
			_ = target
		}
	})
}

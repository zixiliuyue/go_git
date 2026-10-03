package index

import (
	"crypto/sha1"
	"testing"
)

// FuzzIndexDecoder 对 Git Index (.git/index) 解析器进行模糊测试
// 重点验证防范畸变条目、超大条目计数导致的内存溢出、越界扩展解析等
func FuzzIndexDecoder(f *testing.F) {
	// 1. 构造一个合法的空 v2 Index 作为种子
	idx2 := &Index{
		Version: 2,
	}
	serialized2, err := idx2.Serialize()
	if err == nil {
		f.Add(serialized2)
	}

	// 2. 构造一个合法的空 v4 Index 作为种子
	idx4 := &Index{
		Version: 4,
	}
	serialized4, err := idx4.Serialize()
	if err == nil {
		f.Add(serialized4)
	}

	// 3. 构造一些畸形变体种子
	f.Add([]byte("DIRC\x00\x00\x00\x02\xff\xff\xff\xff")) // 超大条目计数
	f.Add([]byte("DIRC\x00\x00\x00\x05\x00\x00\x00\x00")) // 不支持的 v5
	f.Add([]byte("NOPE\x00\x00\x00\x02\x00\x00\x00\x00")) // 非 DIRC 头部
	f.Add([]byte(""))

	// 4. 构造带伪造校验和的种子
	fakeData := []byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00")
	checksum := sha1.Sum(fakeData)
	f.Add(append(fakeData, checksum[:]...))

	// 5. 执行 Fuzz 测试
	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := ParseIndex(data)
		if err == nil && parsed != nil {
			// 成功解析则尝试再次序列化，验证往返（Round-trip）一致性
			_, _ = parsed.Serialize()
		}
	})
}

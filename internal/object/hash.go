package object

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
)

// Hash 表示 Git 的 20 字节 SHA-1 哈希值。
// 在 Git 内部所有对象（blob/tree/commit/tag）均以此唯一标识。
type Hash [20]byte

// ZeroHash 为全零哈希，通常表示不存在的父提交或空对象。
var ZeroHash Hash

// NewHashFromHex 从 40 字符十六进制字符串解析 Hash。
// 如果格式不合法或长度不足 40 位，将返回清晰的错误信息。
func NewHashFromHex(s string) (Hash, error) {
	if len(s) != 40 {
		return ZeroHash, fmt.Errorf("无效的哈希长度: 期望 40 字符，实际为 %d (%s)", len(s), s)
	}
	var h Hash
	n, err := hex.Decode(h[:], []byte(s))
	if err != nil {
		return ZeroHash, fmt.Errorf("解析十六进制哈希失败: %w", err)
	}
	if n != 20 {
		return ZeroHash, fmt.Errorf("解码哈希字节数不足: 期望 20 字节，实际 %d", n)
	}
	return h, nil
}

// MustHashFromHex 从十六进制字符串解析 Hash，遇到错误时 panic。
// 仅建议在单元测试或确定合法的常量哈希转换中使用。
func MustHashFromHex(s string) Hash {
	h, err := NewHashFromHex(s)
	if err != nil {
		panic(err)
	}
	return h
}

// String 返回 40 字符小写十六进制字符串。
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// Short 返回 7 字符缩写哈希（Git 默认简写格式）。
func (h Hash) Short() string {
	s := h.String()
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

// IsZero 判断当前哈希是否为全零。
func (h Hash) IsZero() bool {
	return h == ZeroHash
}

// ComputeHash 计算给定数据的 SHA-1 校验和。
func ComputeHash(data []byte) Hash {
	return sha1.Sum(data)
}

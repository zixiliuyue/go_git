package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// ObjectType 表示 Git 对象的四种基础类型之一。
type ObjectType string

const (
	TypeBlob   ObjectType = "blob"
	TypeTree   ObjectType = "tree"
	TypeCommit ObjectType = "commit"
	TypeTag    ObjectType = "tag"
)

// Object 是所有 Git 对象的通用接口。
type Object interface {
	// Type 返回对象的具体类型 (blob/tree/commit/tag)
	Type() ObjectType
	// Payload 返回对象未加头的原生内容字节
	Payload() []byte
	// Hash 返回对象的 20 字节 SHA-1 值（计算方式为 sha1("<type> <len>\0<payload>")）
	Hash() Hash
}

// RawObject 表示尚未具体解析为结构体的原始 Git 对象。
type RawObject struct {
	ObjType ObjectType
	Content []byte
	hash    Hash
}

// Type 实现 Object 接口。
func (r *RawObject) Type() ObjectType {
	return r.ObjType
}

// Payload 实现 Object 接口。
func (r *RawObject) Payload() []byte {
	return r.Content
}

// Hash 实现 Object 接口。
func (r *RawObject) Hash() Hash {
	if r.hash.IsZero() && (len(r.Content) > 0 || r.ObjType != "") {
		r.hash = HashObject(r.ObjType, r.Content)
	}
	return r.hash
}

// HashObject 按照 Git 规范计算对象的 SHA-1 哈希：
// 格式为: sha1("<type> <size>\x00<content>")
func HashObject(objType ObjectType, content []byte) Hash {
	h := sha1.New()
	// 写入 Git 规范对象头
	header := fmt.Sprintf("%s %d\x00", objType, len(content))
	h.Write([]byte(header))
	h.Write(content)
	var out Hash
	copy(out[:], h.Sum(nil))
	return out
}

// FormatObject 构建完整的 loose 对象未压缩字节：header + null + content。
func FormatObject(objType ObjectType, content []byte) []byte {
	header := fmt.Sprintf("%s %d\x00", objType, len(content))
	buf := make([]byte, 0, len(header)+len(content))
	buf = append(buf, header...)
	buf = append(buf, content...)
	return buf
}

// ParseObjectHeader 从原始未压缩数据流中解析 Git 对象头。
// 返回对象类型、内容长度以及内容起始偏移量。
func ParseObjectHeader(data []byte) (objType ObjectType, size int64, headerLen int, err error) {
	nullIdx := bytes.IndexByte(data, 0)
	if nullIdx < 0 {
		return "", 0, 0, errors.New("无效的对象头：缺少空字节分隔符")
	}

	headerStr := string(data[:nullIdx])
	spaceIdx := bytes.IndexByte(data[:nullIdx], ' ')
	if spaceIdx < 0 {
		return "", 0, 0, fmt.Errorf("无效的对象头格式: %q", headerStr)
	}

	typeStr := ObjectType(headerStr[:spaceIdx])
	switch typeStr {
	case TypeBlob, TypeTree, TypeCommit, TypeTag:
	default:
		return "", 0, 0, fmt.Errorf("未知的对象类型: %q", typeStr)
	}

	sizeInt, err := strconv.ParseInt(headerStr[spaceIdx+1:], 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("解析对象大小失败: %w", err)
	}
	if sizeInt < 0 {
		return "", 0, 0, fmt.Errorf("对象大小为负数: %d", sizeInt)
	}

	return typeStr, sizeInt, nullIdx + 1, nil
}

// ReadLooseObject 从 loose 对象的压缩文件中解压缩并解析为 RawObject。
func ReadLooseObject(path string) (*RawObject, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 loose 对象文件失败: %w", err)
	}
	defer f.Close()

	return ReadLooseObjectFromReader(f)
}

// ReadLooseObjectFromReader 从任意 io.Reader 读取 zlib 压缩的 loose 对象数据。
func ReadLooseObjectFromReader(r io.Reader) (*RawObject, error) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("创建 zlib 解压 reader 失败: %w", err)
	}
	defer zr.Close()

	decompressed, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("解压 loose 对象内容失败: %w", err)
	}

	objType, size, headerLen, err := ParseObjectHeader(decompressed)
	if err != nil {
		return nil, err
	}

	content := decompressed[headerLen:]
	if int64(len(content)) != size {
		return nil, fmt.Errorf("对象大小不匹配: 头声明 %d 字节，实际得到 %d 字节", size, len(content))
	}

	// 计算哈希确保数据完整性
	h := HashObject(objType, content)

	return &RawObject{
		ObjType: objType,
		Content: content,
		hash:    h,
	}, nil
}

// WriteLooseObjectToDir 将对象写入指定 `.git/objects` 目录，路径为 `objects/xx/yyyy...`。
// 为了保证原子性，先写入同目录下的临时文件，再原子 rename。
func WriteLooseObjectToDir(objectsDir string, objType ObjectType, content []byte) (Hash, error) {
	h := HashObject(objType, content)
	hStr := h.String()
	subDir := filepath.Join(objectsDir, hStr[:2])
	objPath := filepath.Join(subDir, hStr[2:])

	// 若对象已存在，Git 默认幂等跳过写入
	if _, err := os.Stat(objPath); err == nil {
		return h, nil
	}

	// 确保父目录存在
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return ZeroHash, fmt.Errorf("创建对象目录失败: %w", err)
	}

	// 创建临时文件写入 zlib 压缩流
	tmpFile, err := os.CreateTemp(subDir, "tmp_obj_*")
	if err != nil {
		return ZeroHash, fmt.Errorf("创建临时对象文件失败: %w", err)
	}
	tmpName := tmpFile.Name()

	// 发生错误时确保临时文件被清理
	cleanup := true
	defer func() {
		if cleanup {
			tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	zw := zlib.NewWriter(tmpFile)
	// 写入 Git 规范对象头
	header := fmt.Sprintf("%s %d\x00", objType, len(content))
	if _, err := zw.Write([]byte(header)); err != nil {
		return ZeroHash, fmt.Errorf("写入对象头失败: %w", err)
	}
	if _, err := zw.Write(content); err != nil {
		return ZeroHash, fmt.Errorf("写入对象内容失败: %w", err)
	}
	if err := zw.Close(); err != nil {
		return ZeroHash, fmt.Errorf("关闭 zlib writer 失败: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return ZeroHash, fmt.Errorf("关闭临时文件失败: %w", err)
	}

	// 设置 git 常用权限 0444
	_ = os.Chmod(tmpName, 0444)

	// 原子替换
	if err := os.Rename(tmpName, objPath); err != nil {
		// 并发下如果目标已被创建，忽略 rename 错误（幂等）
		if _, statErr := os.Stat(objPath); statErr == nil {
			cleanup = true
			return h, nil
		}
		return ZeroHash, fmt.Errorf("重命名临时对象文件失败: %w", err)
	}

	cleanup = false
	return h, nil
}

package object

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// FileMode 表示 Git tree 中的文件权限模式。
// Git 支持的主要模式有：
// 040000 (目录 / sub-tree)
// 100644 (普通文件)
// 100755 (可执行文件)
// 120000 (符号链接)
// 160000 (gitlink / submodule)
type FileMode uint32

const (
	ModeDirectory FileMode = 0040000
	ModeRegular   FileMode = 0100644
	ModeExec      FileMode = 0100755
	ModeSymlink   FileMode = 0120000
	ModeSubmodule FileMode = 0160000
)

// String 以八进制形式返回 FileMode。注意 Git 格式规范：
// 目录模式输出为 "40000"（无前导0），文件模式输出为 "100644" 等。
func (m FileMode) String() string {
	return strconv.FormatUint(uint64(m), 8)
}

// TreeEntry 代表 Git Tree 对象中的一个条目。
type TreeEntry struct {
	Mode FileMode
	Name string
	OID  Hash
}

// Tree 表示一个 Git 目录树对象。
type Tree struct {
	Entries []TreeEntry
	raw     []byte
	hash    Hash
}

// Type 实现 Object 接口。
func (t *Tree) Type() ObjectType {
	return TypeTree
}

// Payload 返回 Tree 对象按 Git 规范序列化后的原始数据。
func (t *Tree) Payload() []byte {
	if t.raw == nil {
		t.raw = t.Serialize()
	}
	return t.raw
}

// Hash 返回 Tree 对象的 SHA-1。
func (t *Tree) Hash() Hash {
	if t.hash.IsZero() {
		t.hash = HashObject(TypeTree, t.Payload())
	}
	return t.hash
}

// CompareTreeEntries 依据 Git 规范对比两个条目：
// 如果条目是目录，比较时其名称末尾需虚拟追加 '/'。
func CompareTreeEntries(a, b TreeEntry) int {
	aName := a.Name
	if a.Mode == ModeDirectory {
		aName += "/"
	}
	bName := b.Name
	if b.Mode == ModeDirectory {
		bName += "/"
	}
	if aName < bName {
		return -1
	} else if aName > bName {
		return 1
	}
	return 0
}

// Sort 对 Tree 的所有条目按照 Git 严格规则原地排序。
func (t *Tree) Sort() {
	sort.Slice(t.Entries, func(i, j int) bool {
		return CompareTreeEntries(t.Entries[i], t.Entries[j]) < 0
	})
}

// Serialize 将 Tree 对象序列化为 Git loose tree 二进制格式：
// 重复序列: `<mode(八进制无前导0)> <name>\x00<20字节OID>`
func (t *Tree) Serialize() []byte {
	t.Sort()
	var buf bytes.Buffer
	for _, entry := range t.Entries {
		// 格式: "<mode> <name>\x00"
		modeStr := entry.Mode.String()
		buf.WriteString(modeStr)
		buf.WriteByte(' ')
		buf.WriteString(entry.Name)
		buf.WriteByte(0)
		buf.Write(entry.OID[:])
	}
	return buf.Bytes()
}

// ParseTree 从原始二进制数据中解析 Tree 对象。
func ParseTree(data []byte) (*Tree, error) {
	var entries []TreeEntry
	cur := data
	for len(cur) > 0 {
		spaceIdx := bytes.IndexByte(cur, ' ')
		if spaceIdx < 0 {
			return nil, errors.New("Tree 条目格式错误: 找不到空格分隔符")
		}
		modeStr := string(cur[:spaceIdx])
		modeVal, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("解析 Tree 条目权限失败 (%s): %w", modeStr, err)
		}

		nullIdx := bytes.IndexByte(cur[spaceIdx+1:], 0)
		if nullIdx < 0 {
			return nil, errors.New("Tree 条目格式错误: 找不到名称结束符空字节")
		}
		nameStart := spaceIdx + 1
		nameEnd := nameStart + nullIdx
		name := string(cur[nameStart:nameEnd])

		oidStart := nameEnd + 1
		if len(cur) < oidStart+20 {
			return nil, errors.New("Tree 条目数据截断: 不足 20 字节 OID")
		}
		var oid Hash
		copy(oid[:], cur[oidStart:oidStart+20])

		entries = append(entries, TreeEntry{
			Mode: FileMode(modeVal),
			Name: name,
			OID:  oid,
		})

		cur = cur[oidStart+20:]
	}

	tree := &Tree{
		Entries: entries,
		raw:     data,
		hash:    HashObject(TypeTree, data),
	}
	return tree, nil
}

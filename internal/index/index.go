package index

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"os"
	"sort"
	"strings"
)

// IndexHeaderMagic Git 索引文件魔数 "DIRC"
var IndexHeaderMagic = [4]byte{'D', 'I', 'R', 'C'}

// IndexExtension 代表可选的索引扩展块（如 TREE, link, UNTR 等）
type IndexExtension struct {
	Signature [4]byte
	Data      []byte
}

// Index 代表整个 Git 索引文件（staging area）。
type Index struct {
	Version    uint32
	Entries    []*IndexEntry
	Extensions []IndexExtension
	Checksum   object.Hash
}

// NewIndex 创建一个空的 Git 索引实例，默认版本为 v2。
func NewIndex() *Index {
	return &Index{
		Version: 2,
		Entries: make([]*IndexEntry, 0),
	}
}

// ReadIndex 从文件路径（例如 .git/index）读取并解析 Git 索引。
// 如果文件不存在，则返回一个空的 Index。
func ReadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewIndex(), nil
		}
		return nil, fmt.Errorf("读取索引文件失败: %w", err)
	}

	return ParseIndex(data)
}

// ParseIndex 从原始字节流中解析 Git 索引文件。
func ParseIndex(data []byte) (*Index, error) {
	if len(data) < 12+20 { // 12字节头部 + 至少20字节SHA-1尾部
		return nil, errors.New("索引文件过小，不符合 DIRC 格式规范")
	}

	// 校验尾部 20 字节 SHA-1
	contentLen := len(data) - 20
	expectedChecksum := sha1.Sum(data[:contentLen])
	if !bytes.Equal(expectedChecksum[:], data[contentLen:]) {
		return nil, errors.New("索引文件校验和损坏 (SHA-1 mismatch)")
	}

	// 解析头部
	if !bytes.Equal(data[0:4], IndexHeaderMagic[:]) {
		return nil, fmt.Errorf("非法的索引魔数: %v", data[0:4])
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("不支持的索引版本: %d (仅支持 v2/v3/v4)", version)
	}
	numEntries := binary.BigEndian.Uint32(data[8:12])

	idx := &Index{
		Version:  version,
		Entries:  make([]*IndexEntry, 0, numEntries),
		Checksum: object.Hash(expectedChecksum),
	}

	cur := data[12:contentLen]
	for i := uint32(0); i < numEntries; i++ {
		entry, consumed, err := ParseEntry(cur)
		if err != nil {
			return nil, fmt.Errorf("解析第 %d 个条目失败: %w", i, err)
		}
		idx.Entries = append(idx.Entries, entry)
		cur = cur[consumed:]
	}

	// 解析可选扩展块
	for len(cur) >= 8 {
		var sig [4]byte
		copy(sig[:], cur[0:4])
		extLen := binary.BigEndian.Uint32(cur[4:8])
		if uint32(len(cur)-8) < extLen {
			break
		}
		extData := make([]byte, extLen)
		copy(extData, cur[8:8+extLen])
		idx.Extensions = append(idx.Extensions, IndexExtension{
			Signature: sig,
			Data:      extData,
		})
		cur = cur[8+extLen:]
	}

	return idx, nil
}

// Serialize 按照 Git DIRC 规范将索引序列化为完整二进制流（包含头部、条目、扩展与校验和）。
func (idx *Index) Serialize() ([]byte, error) {
	// 确保所有条目已按路径与阶段升序排序
	idx.Sort()

	var buf bytes.Buffer

	// 1. 写入头部 12 字节
	buf.Write(IndexHeaderMagic[:])
	var verBuf [4]byte
	binary.BigEndian.PutUint32(verBuf[:], idx.Version)
	buf.Write(verBuf[:])

	var numBuf [4]byte
	binary.BigEndian.PutUint32(numBuf[:], uint32(len(idx.Entries)))
	buf.Write(numBuf[:])

	// 2. 写入条目
	for _, entry := range idx.Entries {
		buf.Write(entry.Serialize())
	}

	// 3. 写入扩展块
	for _, ext := range idx.Extensions {
		buf.Write(ext.Signature[:])
		var extLenBuf [4]byte
		binary.BigEndian.PutUint32(extLenBuf[:], uint32(len(ext.Data)))
		buf.Write(extLenBuf[:])
		buf.Write(ext.Data)
	}

	// 4. 计算并追加 20 字节 SHA-1 校验和
	checksum := sha1.Sum(buf.Bytes())
	buf.Write(checksum[:])
	idx.Checksum = object.Hash(checksum)

	return buf.Bytes(), nil
}

// WriteIndex 将索引安全原子地写入磁盘路径（使用 .lock 机制与重命名）。
func (idx *Index) WriteIndex(path string) error {
	lockPath := path + ".lock"

	// 独占创建 lock 文件
	lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("无法锁定索引文件 %s: %w", lockPath, err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			lockFile.Close()
			_ = os.Remove(lockPath)
		}
	}()

	data, err := idx.Serialize()
	if err != nil {
		return fmt.Errorf("序列化索引失败: %w", err)
	}

	if _, err := lockFile.Write(data); err != nil {
		return fmt.Errorf("写入索引锁文件失败: %w", err)
	}
	if err := lockFile.Close(); err != nil {
		return fmt.Errorf("关闭索引锁文件失败: %w", err)
	}

	// 原子替换原 index 文件
	if err := os.Rename(lockPath, path); err != nil {
		return fmt.Errorf("原子重命名索引文件失败: %w", err)
	}

	cleanup = false
	return nil
}

// CompareEntries 对比两个 IndexEntry：依据路径字节序升序，同路径依据 stage 升序
func CompareEntries(a, b *IndexEntry) int {
	if a.Path < b.Path {
		return -1
	} else if a.Path > b.Path {
		return 1
	}
	aStage := a.Stage()
	bStage := b.Stage()
	if aStage < bStage {
		return -1
	} else if aStage > bStage {
		return 1
	}
	return 0
}

// Sort 对索引条目按 Git 规范排序。
func (idx *Index) Sort() {
	sort.Slice(idx.Entries, func(i, j int) bool {
		return CompareEntries(idx.Entries[i], idx.Entries[j]) < 0
	})
}

// Find 依据路径二分查找 stage 0 的条目；若不存在返回 nil。
func (idx *Index) Find(path string) *IndexEntry {
	i := sort.Search(len(idx.Entries), func(i int) bool {
		return idx.Entries[i].Path >= path
	})
	if i < len(idx.Entries) && idx.Entries[i].Path == path && idx.Entries[i].Stage() == 0 {
		return idx.Entries[i]
	}
	return nil
}

// AddOrReplaceEntry 添加新条目或替换同路径同 stage 的旧条目，保持排序。
func (idx *Index) AddOrReplaceEntry(newEntry *IndexEntry) {
	for i, existing := range idx.Entries {
		if existing.Path == newEntry.Path && existing.Stage() == newEntry.Stage() {
			idx.Entries[i] = newEntry
			return
		}
	}
	idx.Entries = append(idx.Entries, newEntry)
	idx.Sort()
}

// RemoveEntry 从索引中移除指定路径的所有 stage 条目。
func (idx *Index) RemoveEntry(path string) bool {
	removed := false
	filtered := idx.Entries[:0]
	for _, e := range idx.Entries {
		if e.Path == path {
			removed = true
		} else {
			filtered = append(filtered, e)
		}
	}
	idx.Entries = filtered
	return removed
}

// WriteTree 将当前索引结构递归构建为 Git Tree 对象并保存到 objects 目录，返回根树哈希。
func (idx *Index) WriteTree(objectsDir string) (object.Hash, error) {
	// 递归树节点结构
	type treeNode struct {
		subtrees map[string]*treeNode
		files    map[string]*IndexEntry
	}

	newNode := func() *treeNode {
		return &treeNode{
			subtrees: make(map[string]*treeNode),
			files:    make(map[string]*IndexEntry),
		}
	}

	root := newNode()

	// 将所有暂存条目按路径分隔构建树形内存结构
	for _, entry := range idx.Entries {
		if entry.Stage() != 0 {
			return object.ZeroHash, fmt.Errorf("无法从含冲突未解决的索引生成 Tree (文件 %s 处于 stage %d)", entry.Path, entry.Stage())
		}
		parts := strings.Split(entry.Path, "/")
		curr := root
		for i := 0; i < len(parts)-1; i++ {
			dirName := parts[i]
			if _, ok := curr.subtrees[dirName]; !ok {
				curr.subtrees[dirName] = newNode()
			}
			curr = curr.subtrees[dirName]
		}
		fileName := parts[len(parts)-1]
		curr.files[fileName] = entry
	}

	// 递归序列化节点并写入 loose 存储
	var buildTree func(node *treeNode) (object.Hash, error)
	buildTree = func(node *treeNode) (object.Hash, error) {
		var treeEntries []object.TreeEntry

		// 处理子目录
		for dirName, sub := range node.subtrees {
			subHash, err := buildTree(sub)
			if err != nil {
				return object.ZeroHash, err
			}
			treeEntries = append(treeEntries, object.TreeEntry{
				Mode: object.ModeDirectory,
				Name: dirName,
				OID:  subHash,
			})
		}

		// 处理文件条目
		for fileName, entry := range node.files {
			treeEntries = append(treeEntries, object.TreeEntry{
				Mode: object.FileMode(entry.Mode),
				Name: fileName,
				OID:  entry.OID,
			})
		}

		tree := &object.Tree{Entries: treeEntries}
		// 写入 objects 目录
		treeHash, err := object.WriteLooseObjectToDir(objectsDir, object.TypeTree, tree.Payload())
		if err != nil {
			return object.ZeroHash, fmt.Errorf("写入 Tree 对象失败: %w", err)
		}
		return treeHash, nil
	}

	return buildTree(root)
}

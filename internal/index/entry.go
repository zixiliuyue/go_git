package index

import (
	"encoding/binary"
	"gogit/internal/object"
	"os"
	"syscall"
)

// IndexEntry 代表 Git 索引（index/staging area）中的一个文件条目。
type IndexEntry struct {
	CtimeSeconds  uint32
	CtimeNanosecs uint32
	MtimeSeconds  uint32
	MtimeNanosecs uint32
	Dev           uint32
	Ino           uint32
	Mode          uint32
	UID           uint32
	GID           uint32
	Size          uint32
	OID           object.Hash
	Flags         uint16
	ExtendedFlags uint16
	Path          string
}

// Stage 返回条目的暂存阶段（0 为普通，1 为基础版本，2 为 ours，3 为 theirs）。
func (e *IndexEntry) Stage() int {
	return int((e.Flags >> 12) & 0x03)
}

// SetStage 设置暂存阶段（0-3）。
func (e *IndexEntry) SetStage(stage int) {
	e.Flags = (e.Flags & ^uint16(0x3000)) | (uint16(stage&0x03) << 12)
}

// IsSkipWorktree 返回当前条目是否被标记为 skip-worktree（用于稀疏检出）
func (e *IndexEntry) IsSkipWorktree() bool {
	return (e.ExtendedFlags & 0x4000) != 0
}

// SetSkipWorktree 设置或清除 skip-worktree 标志位
func (e *IndexEntry) SetSkipWorktree(skip bool) {
	if skip {
		e.Flags |= 0x4000
		e.ExtendedFlags |= 0x4000
	} else {
		e.ExtendedFlags &^= 0x4000
	}
}

// EntryFromOSFileInfo 从操作系统文件状态构建 IndexEntry 元数据。
// 用于实现 stat 缓存比对与暂存更新。
func EntryFromOSFileInfo(path string, fi os.FileInfo, oid object.Hash) *IndexEntry {
	entry := &IndexEntry{
		OID:  oid,
		Path: path,
	}

	mtime := fi.ModTime()
	entry.MtimeSeconds = uint32(mtime.Unix())
	entry.MtimeNanosecs = uint32(mtime.Nanosecond())

	// 默认文件大小截断为 32 位无符号整数
	entry.Size = uint32(fi.Size())

	// 确定 Git 模式：0100755（可执行）或 0100644（普通文件）或 0120000（符号链接）
	if fi.Mode()&os.ModeSymlink != 0 {
		entry.Mode = uint32(object.ModeSymlink)
	} else if fi.Mode()&0111 != 0 {
		entry.Mode = uint32(object.ModeExec)
	} else {
		entry.Mode = uint32(object.ModeRegular)
	}

	// 提取操作系统特定 stat 字段（dev, ino, uid, gid, ctime）
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		entry.Dev = uint32(stat.Dev)
		entry.Ino = uint32(stat.Ino)
		entry.UID = stat.Uid
		entry.GID = stat.Gid
		// macOS 与 Linux stat 结构体字段存在平台差异，进行跨平台安全获取
		setStatCtime(entry, stat)
	} else {
		// 回退使用 mtime 填充 ctime
		entry.CtimeSeconds = entry.MtimeSeconds
		entry.CtimeNanosecs = entry.MtimeNanosecs
	}

	// 设置 Flags：name length 最大 0xFFF
	nameLen := len(path)
	if nameLen > 0xFFF {
		nameLen = 0xFFF
	}
	entry.Flags = uint16(nameLen)

	return entry
}

// MatchStat 检查工作区文件的当前 stat 是否与索引条目缓存相符。
// 如果匹配，说明文件未经修改，无需重新哈希读取文件内容。
func (e *IndexEntry) MatchStat(fi os.FileInfo) bool {
	// 大小不匹配直接判定修改
	if uint32(fi.Size()) != e.Size {
		return false
	}

	// 修改时间比较
	mtime := fi.ModTime()
	if uint32(mtime.Unix()) != e.MtimeSeconds || uint32(mtime.Nanosecond()) != e.MtimeNanosecs {
		return false
	}

	// 权限模式比对（主要区分可执行位与普通文件）
	var currentMode uint32
	if fi.Mode()&os.ModeSymlink != 0 {
		currentMode = uint32(object.ModeSymlink)
	} else if fi.Mode()&0111 != 0 {
		currentMode = uint32(object.ModeExec)
	} else {
		currentMode = uint32(object.ModeRegular)
	}
	if currentMode != e.Mode {
		return false
	}

	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if uint32(stat.Ino) != e.Ino || uint32(stat.Dev) != e.Dev {
			return false
		}
	}

	return true
}

// SerializeEntry 按照 Git index v2 规范序列化单个条目：
// 包含 62 字节定长头 + 路径 + 1-8 字节空字节填充以对齐至 8 字节边界。
func (e *IndexEntry) Serialize() []byte {
	pathBytes := []byte(e.Path)
	pathLen := len(pathBytes)

	// 计算填充字节数使得条目总长度为 8 的倍数
	// 62 是头部定长字节数
	padLen := 8 - ((62 + pathLen) % 8)
	totalLen := 62 + pathLen + padLen

	buf := make([]byte, totalLen)
	binary.BigEndian.PutUint32(buf[0:4], e.CtimeSeconds)
	binary.BigEndian.PutUint32(buf[4:8], e.CtimeNanosecs)
	binary.BigEndian.PutUint32(buf[8:12], e.MtimeSeconds)
	binary.BigEndian.PutUint32(buf[12:16], e.MtimeNanosecs)
	binary.BigEndian.PutUint32(buf[16:20], e.Dev)
	binary.BigEndian.PutUint32(buf[20:24], e.Ino)
	binary.BigEndian.PutUint32(buf[24:28], e.Mode)
	binary.BigEndian.PutUint32(buf[28:32], e.UID)
	binary.BigEndian.PutUint32(buf[32:36], e.GID)
	binary.BigEndian.PutUint32(buf[36:40], e.Size)
	copy(buf[40:60], e.OID[:])

	// 确保 Flags 中的 name 长度准确
	nameLenField := pathLen
	if nameLenField > 0xFFF {
		nameLenField = 0xFFF
	}
	flags := (e.Flags & 0xF000) | uint16(nameLenField)
	binary.BigEndian.PutUint16(buf[60:62], flags)

	copy(buf[62:62+pathLen], pathBytes)
	// 其余字节 buf 中自动为 0，正好充当 null 结束符与对齐填充

	return buf
}

// ParseEntry 解析单个 index v2 格式条目，返回解析出的条目与消耗的字节数。
func ParseEntry(data []byte) (*IndexEntry, int, error) {
	if len(data) < 62 {
		return nil, 0, os.ErrInvalid
	}

	e := &IndexEntry{
		CtimeSeconds:  binary.BigEndian.Uint32(data[0:4]),
		CtimeNanosecs: binary.BigEndian.Uint32(data[4:8]),
		MtimeSeconds:  binary.BigEndian.Uint32(data[8:12]),
		MtimeNanosecs: binary.BigEndian.Uint32(data[12:16]),
		Dev:           binary.BigEndian.Uint32(data[16:20]),
		Ino:           binary.BigEndian.Uint32(data[20:24]),
		Mode:          binary.BigEndian.Uint32(data[24:28]),
		UID:           binary.BigEndian.Uint32(data[28:32]),
		GID:           binary.BigEndian.Uint32(data[32:36]),
		Size:          binary.BigEndian.Uint32(data[36:40]),
		Flags:         binary.BigEndian.Uint16(data[60:62]),
	}
	copy(e.OID[:], data[40:60])

	// 查找路径结束符 '\0'
	pathStart := 62
	pathEnd := -1
	for i := pathStart; i < len(data); i++ {
		if data[i] == 0 {
			pathEnd = i
			break
		}
	}
	if pathEnd < 0 {
		return nil, 0, os.ErrInvalid
	}

	e.Path = string(data[pathStart:pathEnd])
	pathLen := len(e.Path)
	padLen := 8 - ((62 + pathLen) % 8)
	totalLen := 62 + pathLen + padLen

	if len(data) < totalLen {
		return nil, 0, os.ErrInvalid
	}

	return e, totalLen, nil
}

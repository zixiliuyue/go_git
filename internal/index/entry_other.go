//go:build !darwin

package index

import (
	"syscall"
)

// setStatCtime 通用类 Unix/Linux 平台的 ctime 提取实现
func setStatCtime(entry *IndexEntry, stat *syscall.Stat_t) {
	// 回退使用 mtime 填充 ctime
	entry.CtimeSeconds = entry.MtimeSeconds
	entry.CtimeNanosecs = entry.MtimeNanosecs
}

//go:build darwin

package index

import (
	"syscall"
)

// setStatCtime 获取 macOS 平台的 ctime 时间戳
func setStatCtime(entry *IndexEntry, stat *syscall.Stat_t) {
	entry.CtimeSeconds = uint32(stat.Ctimespec.Sec)
	entry.CtimeNanosecs = uint32(stat.Ctimespec.Nsec)
}

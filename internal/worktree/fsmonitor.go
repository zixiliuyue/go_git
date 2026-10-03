package worktree

import (
	"bytes"
	"fmt"
	"gogit/internal/index"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// IsFSMonitorEnabled 检查仓库配置中是否启用了 core.fsmonitor
func IsFSMonitorEnabled(r *repo.Repository) bool {
	return r.Config.Get("core", "fsmonitor") != ""
}

// QueryFSMonitor 执行 FSMonitor 守护进程/钩子查询
// 返回 (newToken, changedFiles, isFullRefresh, error)
func QueryFSMonitor(r *repo.Repository, prevToken string) (string, map[string]bool, bool, error) {
	hookCmd := r.Config.Get("core", "fsmonitor")
	if hookCmd == "" {
		return "", nil, false, nil
	}

	// 解析钩子执行文件绝对路径
	execPath := hookCmd
	if !filepath.IsAbs(execPath) {
		// 优先相对 WorkTree，若不存在则检查 GitDir
		candidate := filepath.Join(r.WorkTree, execPath)
		if _, err := os.Stat(candidate); err == nil {
			execPath = candidate
		} else {
			execPath = filepath.Join(r.GitDir, execPath)
		}
	}

	version := "2"
	if customVer := r.Config.Get("core", "fsmonitorhookversion"); customVer != "" {
		version = customVer
	}

	token := prevToken
	if token == "" {
		// 初始时使用当前纳秒级时间戳作为时钟基线
		token = strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	cmd := exec.Command(execPath, version, token)
	cmd.Dir = r.WorkTree
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GIT_DIR=%s", r.GitDir),
		fmt.Sprintf("GIT_WORK_TREE=%s", r.WorkTree),
	)

	outBytes, err := cmd.Output()
	if err != nil {
		// 守护进程异常时降级为全量刷新
		return "", nil, true, fmt.Errorf("fsmonitor hook 执行失败: %w", err)
	}

	// 解析输出内容（支持以换行或 null 分隔）
	var parts []string
	if bytes.IndexByte(outBytes, 0) >= 0 {
		rawParts := bytes.Split(outBytes, []byte{0})
		for _, p := range rawParts {
			s := string(bytes.TrimSpace(p))
			if s != "" {
				parts = append(parts, s)
			}
		}
	} else {
		lines := strings.Split(string(outBytes), "\n")
		for _, l := range lines {
			s := strings.TrimSpace(l)
			if s != "" {
				parts = append(parts, s)
			}
		}
	}

	if len(parts) == 0 {
		return "", nil, true, nil
	}

	newToken := parts[0]
	// Git 规范：若 token 以 '/' 开头，代表守护进程要求对工作区进行全量刷新重扫
	if strings.HasPrefix(newToken, "/") {
		return strings.TrimPrefix(newToken, "/"), nil, true, nil
	}

	changedFiles := make(map[string]bool)
	for _, p := range parts[1:] {
		cleaned := filepath.ToSlash(filepath.Clean(p))
		changedFiles[cleaned] = true
	}

	return newToken, changedFiles, false, nil
}

// ApplyFSMonitorUpdate 执行 FSMonitor 查询并将变更标记同步回索引
func ApplyFSMonitorUpdate(r *repo.Repository, idx *index.Index) (bool, error) {
	if !IsFSMonitorEnabled(r) {
		return false, nil
	}

	prevToken := ""
	fsmn, _ := idx.GetFSMonitorExtension()
	if fsmn != nil {
		prevToken = fsmn.Token
	}

	newToken, changedFiles, isFullRefresh, err := QueryFSMonitor(r, prevToken)
	if err != nil {
		// 容错降级：不阻断主流程，仅将条目有效位置 0
		for _, entry := range idx.Entries {
			entry.SetFSMonitorValid(false)
		}
		return false, err
	}

	if isFullRefresh {
		for _, entry := range idx.Entries {
			entry.SetFSMonitorValid(false)
		}
	} else {
		for _, entry := range idx.Entries {
			if changedFiles != nil && changedFiles[entry.Path] {
				entry.SetFSMonitorValid(false)
			} else {
				entry.SetFSMonitorValid(true)
			}
		}
	}

	idx.SetFSMonitorExtension(&index.FSMonitorExtension{
		Version: 2,
		Token:   newToken,
	})

	return true, nil
}

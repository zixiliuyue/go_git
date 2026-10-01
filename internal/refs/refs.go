package refs

import (
	"bufio"
	"errors"
	"fmt"
	"gogit/internal/object"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrRefNotFound = errors.New("reference not found")
)

// Ref 代表一个 Git 引用，可以是符号引用（如 HEAD 指向分支）或直接哈希引用。
type Ref struct {
	Name      string      // 完整名称，如 "HEAD", "refs/heads/main"
	Hash      object.Hash // 目标对象的 SHA-1
	Target    string      // 如果是符号引用，指向的目标引用名称（例如 "refs/heads/main"）
	IsSymref  bool        // 是否为符号引用
	Peeled    object.Hash // 若为附注标签，剥离后的目标对象（通常是 commit）哈希
	HasPeeled bool
}

// Manager 负责管理特定仓库目录下的所有引用读写、packed-refs 与 reflog。
type Manager struct {
	gitDir string
}

// NewManager 创建引用管理器。
func NewManager(gitDir string) *Manager {
	return &Manager{gitDir: gitDir}
}

// GitDir 返回当前引用的 .git 目录绝对/相对路径。
func (m *Manager) GitDir() string {
	return m.gitDir
}

// ResolveHEAD 解析当前 HEAD 所最终指向的提交对象哈希。
// 若当前分支尚无任何提交（孤儿分支/初始建仓状态），返回 ErrRefNotFound。
func (m *Manager) ResolveHEAD() (object.Hash, error) {
	return m.ResolveRef("HEAD")
}

// ReadHEAD 读取 HEAD 引用的元数据信息（包括是否为符号引用及其指向分支）。
func (m *Manager) ReadHEAD() (*Ref, error) {
	headPath := filepath.Join(m.gitDir, "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return nil, fmt.Errorf("读取 HEAD 失败: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "ref:") {
		target := strings.TrimSpace(trimmed[4:])
		ref := &Ref{
			Name:     "HEAD",
			Target:   target,
			IsSymref: true,
		}
		// 尝试解析目标引用的哈希
		if h, err := m.ResolveRef(target); err == nil {
			ref.Hash = h
		}
		return ref, nil
	}

	// detached HEAD 状态，直接为 40 位十六进制哈希
	h, err := object.NewHashFromHex(trimmed)
	if err != nil {
		return nil, fmt.Errorf("解析 detached HEAD 哈希失败: %w", err)
	}

	return &Ref{
		Name:     "HEAD",
		Hash:     h,
		IsSymref: false,
	}, nil
}

// SetHEADSymbolic 设置 HEAD 为符号引用，指向指定分支（例如 "refs/heads/main"）。
func (m *Manager) SetHEADSymbolic(targetBranch string) error {
	headPath := filepath.Join(m.gitDir, "HEAD")
	content := fmt.Sprintf("ref: %s\n", targetBranch)
	return atomicWriteFile(headPath, []byte(content), 0644)
}

// SetHEADDetached 设置 HEAD 为分离头指针状态，直接指向指定提交哈希。
func (m *Manager) SetHEADDetached(commitHash object.Hash) error {
	headPath := filepath.Join(m.gitDir, "HEAD")
	content := fmt.Sprintf("%s\n", commitHash.String())
	return atomicWriteFile(headPath, []byte(content), 0644)
}

// ResolveRef 解析指定引用全名或简写（如 "HEAD", "main", "refs/heads/main"），最终追踪到目标 SHA-1。
func (m *Manager) ResolveRef(refName string) (object.Hash, error) {
	visited := make(map[string]bool)
	curr := refName

	for {
		if visited[curr] {
			return object.ZeroHash, fmt.Errorf("检测到引用符号循环引用: %s", curr)
		}
		visited[curr] = true

		ref, err := m.GetRef(curr)
		if err != nil {
			return object.ZeroHash, err
		}

		if !ref.IsSymref {
			return ref.Hash, nil
		}
		curr = ref.Target
	}
}

// GetRef 获取单个引用的信息。优先查找 loose ref，其次从 packed-refs 查找。
func (m *Manager) GetRef(name string) (*Ref, error) {
	// 如果是 HEAD
	if name == "HEAD" {
		return m.ReadHEAD()
	}

	// 候选可能路径
	candidates := []string{name}
	if !strings.HasPrefix(name, "refs/") {
		candidates = append(candidates,
			"refs/"+name,
			"refs/heads/"+name,
			"refs/tags/"+name,
			"refs/remotes/"+name,
			"refs/remotes/"+name+"/HEAD",
		)
	}

	for _, cand := range candidates {
		// 1. 优先检查 loose ref
		loosePath := filepath.Join(m.gitDir, cand)
		if data, err := os.ReadFile(loosePath); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if strings.HasPrefix(trimmed, "ref:") {
				target := strings.TrimSpace(trimmed[4:])
				return &Ref{
					Name:     cand,
					Target:   target,
					IsSymref: true,
				}, nil
			}
			h, err := object.NewHashFromHex(trimmed)
			if err == nil {
				return &Ref{
					Name:     cand,
					Hash:     h,
					IsSymref: false,
				}, nil
			}
		}
	}

	// 2. 检查 packed-refs
	packedMap, err := m.readPackedRefs()
	if err == nil {
		for _, cand := range candidates {
			if r, ok := packedMap[cand]; ok {
				return r, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrRefNotFound, name)
}

// UpdateRef 原子更新指定引用的目标哈希，并追加 reflog 记录。
func (m *Manager) UpdateRef(name string, newHash object.Hash, committer object.Signature, logMsg string) error {
	// 完整标准化引用名称
	fullRefName := name
	if fullRefName != "HEAD" && !strings.HasPrefix(fullRefName, "refs/") {
		fullRefName = "refs/heads/" + fullRefName
	}

	// 获取旧哈希用于 reflog
	oldHash := object.ZeroHash
	if oldRef, err := m.GetRef(fullRefName); err == nil && !oldRef.IsSymref {
		oldHash = oldRef.Hash
	}

	// 如果更新的是 HEAD，且 HEAD 为符号引用，应递归更新目标分支
	if fullRefName == "HEAD" {
		headRef, err := m.ReadHEAD()
		if err == nil && headRef.IsSymref {
			return m.UpdateRef(headRef.Target, newHash, committer, logMsg)
		}
	}

	refPath := filepath.Join(m.gitDir, fullRefName)
	if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
		return fmt.Errorf("创建引用目录失败: %w", err)
	}

	content := fmt.Sprintf("%s\n", newHash.String())
	if err := atomicWriteFile(refPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入引用文件失败: %w", err)
	}

	// 记录 reflog
	_ = m.AppendReflog(fullRefName, oldHash, newHash, committer, logMsg)
	// 若当前在特定分支上，同时也记录一份到 HEAD reflog
	if fullRefName != "HEAD" {
		_ = m.AppendReflog("HEAD", oldHash, newHash, committer, logMsg)
	}

	return nil
}

// DeleteRef 删除指定引用（同时清理 loose 文件）。
func (m *Manager) DeleteRef(name string) error {
	fullRefName := name
	if !strings.HasPrefix(fullRefName, "refs/") {
		fullRefName = "refs/heads/" + fullRefName
	}
	loosePath := filepath.Join(m.gitDir, fullRefName)
	_ = os.Remove(loosePath)
	return nil
}

// ListRefs 列出所有匹配前缀的有效直接引用映射（前缀如 "refs/heads/", "refs/tags/"）。
func (m *Manager) ListRefs(prefix string) (map[string]object.Hash, error) {
	result := make(map[string]object.Hash)

	// 1. 先读取 packed-refs
	packed, _ := m.readPackedRefs()
	for name, ref := range packed {
		if strings.HasPrefix(name, prefix) {
			result[name] = ref.Hash
		}
	}

	// 2. 遍历 loose 覆盖
	scanDir := filepath.Join(m.gitDir, prefix)
	if _, err := os.Stat(scanDir); err == nil {
		_ = filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(m.gitDir, path)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				trimmed := strings.TrimSpace(string(data))
				if h, err := object.NewHashFromHex(trimmed); err == nil {
					result[rel] = h
				}
			}
			return nil
		})
	}

	return result, nil
}

// readPackedRefs 读取 .git/packed-refs 文件
func (m *Manager) readPackedRefs() (map[string]*Ref, error) {
	packedPath := filepath.Join(m.gitDir, "packed-refs")
	f, err := os.Open(packedPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	refsMap := make(map[string]*Ref)
	scanner := bufio.NewScanner(f)
	var lastRef *Ref

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 剥离行 '^<sha>' 紧随在 tag 引用之后
		if strings.HasPrefix(line, "^") {
			if lastRef != nil {
				if h, err := object.NewHashFromHex(line[1:]); err == nil {
					lastRef.Peeled = h
					lastRef.HasPeeled = true
				}
			}
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			h, err := object.NewHashFromHex(fields[0])
			if err == nil {
				ref := &Ref{
					Name:   fields[1],
					Hash:   h,
					Target: "",
				}
				refsMap[fields[1]] = ref
				lastRef = ref
			}
		}
	}

	return refsMap, scanner.Err()
}

// atomicWriteFile 通过 .lock 临时文件与原子重命名安全写盘。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}

	cleanup := true
	defer func() {
		if cleanup {
			f.Close()
			_ = os.Remove(lockPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Rename(lockPath, path); err != nil {
		return err
	}

	cleanup = false
	return nil
}

// AppendReflog 追加一条记录到 .git/logs/<refName>。
func (m *Manager) AppendReflog(refName string, oldHash, newHash object.Hash, committer object.Signature, message string) error {
	logPath := filepath.Join(m.gitDir, "logs", refName)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if committer.When.IsZero() {
		committer.When = time.Now()
	}
	sigStr := committer.String()

	line := fmt.Sprintf("%s %s %s\t%s\n", oldHash.String(), newHash.String(), sigStr, message)
	_, err = f.WriteString(line)
	return err
}

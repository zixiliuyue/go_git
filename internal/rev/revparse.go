package rev

import (
	"bufio"
	"errors"
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseRevision 在指定仓库中解析任意 Git 修订版本表达式（如 HEAD, main, HEAD~2, HEAD^, 哈希缩写等）。
func ParseRevision(r *repo.Repository, expr string) (object.Hash, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return object.ZeroHash, errors.New("空的修订版本表达式")
	}

	// 1. 处理 @{n} reflog 表达式，形如 HEAD@{0}, main@{1}
	if atIdx := strings.Index(expr, "@{"); atIdx >= 0 && strings.HasSuffix(expr, "}") {
		refPart := expr[:atIdx]
		nStr := expr[atIdx+2 : len(expr)-1]
		n, err := strconv.Atoi(nStr)
		if err != nil {
			return object.ZeroHash, fmt.Errorf("解析 reflog 偏移量失败: %w", err)
		}
		return parseReflogIndex(r, refPart, n)
	}

	// 2. 处理 ~n 或 ^n 后缀
	baseExpr, steps, err := splitRevisionSuffixes(expr)
	if err != nil {
		return object.ZeroHash, err
	}

	// 解析基准引用的哈希
	currHash, err := resolveBaseRevision(r, baseExpr)
	if err != nil {
		return object.ZeroHash, err
	}

	// 沿着 commit 父提交链回溯
	for _, step := range steps {
		commit, err := r.ReadCommit(currHash)
		if err != nil {
			return object.ZeroHash, fmt.Errorf("无法加载提交 %s: %w", currHash.String(), err)
		}

		if step.isTilde {
			// ~n: 沿第一父提交回溯 n 次
			for i := 0; i < step.count; i++ {
				if len(commit.Parents) == 0 {
					return object.ZeroHash, fmt.Errorf("提交 %s 没有更多父提交", currHash.String())
				}
				currHash = commit.Parents[0]
				if i < step.count-1 {
					commit, err = r.ReadCommit(currHash)
					if err != nil {
						return object.ZeroHash, err
					}
				}
			}
		} else {
			// ^n: 选择第 n 个父提交 (1-indexed，默认 ^ 相当于 ^1)
			parentIdx := step.count - 1
			if parentIdx < 0 || parentIdx >= len(commit.Parents) {
				return object.ZeroHash, fmt.Errorf("提交 %s 不存在第 %d 个父提交 (共有 %d 个)", currHash.String(), step.count, len(commit.Parents))
			}
			currHash = commit.Parents[parentIdx]
		}
	}

	return currHash, nil
}

type revStep struct {
	isTilde bool
	count   int
}

func splitRevisionSuffixes(expr string) (string, []revStep, error) {
	var steps []revStep
	curr := expr

	for len(curr) > 0 {
		lastTilde := strings.LastIndexByte(curr, '~')
		lastCaret := strings.LastIndexByte(curr, '^')

		if lastTilde < 0 && lastCaret < 0 {
			break
		}

		if lastTilde > lastCaret {
			numPart := curr[lastTilde+1:]
			count := 1
			if numPart != "" {
				c, err := strconv.Atoi(numPart)
				if err != nil {
					return "", nil, fmt.Errorf("无效的 ~ 后缀: %s", numPart)
				}
				count = c
			}
			steps = append([]revStep{{isTilde: true, count: count}}, steps...)
			curr = curr[:lastTilde]
		} else {
			numPart := curr[lastCaret+1:]
			count := 1
			if numPart != "" {
				c, err := strconv.Atoi(numPart)
				if err != nil {
					return "", nil, fmt.Errorf("无效的 ^ 后缀: %s", numPart)
				}
				count = c
			}
			steps = append([]revStep{{isTilde: false, count: count}}, steps...)
			curr = curr[:lastCaret]
		}
	}

	return curr, steps, nil
}

func resolveBaseRevision(r *repo.Repository, base string) (object.Hash, error) {
	// 40 位十六进制直接匹配
	if len(base) == 40 {
		if h, err := object.NewHashFromHex(base); err == nil {
			return h, nil
		}
	}

	// 尝试通过引用系统解析
	if h, err := r.Refs.ResolveRef(base); err == nil {
		return h, nil
	}

	// 缩写哈希（长度在 4 到 39 之间）前缀匹配
	if len(base) >= 4 && len(base) < 40 {
		if h, err := findObjectByPrefix(r.ObjectsDir, base); err == nil {
			return h, nil
		}
	}

	return object.ZeroHash, fmt.Errorf("无法解析修订版本: %q", base)
}

func findObjectByPrefix(objectsDir string, prefix string) (object.Hash, error) {
	prefix = strings.ToLower(prefix)
	dirPrefix := prefix[:2]
	filePrefix := prefix[2:]

	subDir := filepath.Join(objectsDir, dirPrefix)
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return object.ZeroHash, err
	}

	var matches []object.Hash
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, filePrefix) {
			fullHex := dirPrefix + name
			if h, err := object.NewHashFromHex(fullHex); err == nil {
				matches = append(matches, h)
			}
		}
	}

	if len(matches) == 0 {
		return object.ZeroHash, fmt.Errorf("未找到前缀匹配的对象: %s", prefix)
	}
	if len(matches) > 1 {
		return object.ZeroHash, fmt.Errorf("前缀存在歧义 (%d 个匹配): %s", len(matches), prefix)
	}

	return matches[0], nil
}

func parseReflogIndex(r *repo.Repository, refName string, n int) (object.Hash, error) {
	if refName == "" || refName == "HEAD" {
		refName = "HEAD"
	} else if !strings.HasPrefix(refName, "refs/") {
		refName = "refs/heads/" + refName
	}

	logPath := filepath.Join(r.GitDir, "logs", refName)
	f, err := os.Open(logPath)
	if err != nil {
		return object.ZeroHash, fmt.Errorf("打开 reflog 失败: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// reflog 最新的记录在文件末尾，@{0} 代表最后一行
	targetIdx := len(lines) - 1 - n
	if targetIdx < 0 || targetIdx >= len(lines) {
		return object.ZeroHash, fmt.Errorf("reflog 越界 (共有 %d 条记录，请求 @{%d})", len(lines), n)
	}

	line := lines[targetIdx]
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return object.ZeroHash, errors.New("reflog 行格式错误")
	}

	// 第二个字段为 newHash
	return object.NewHashFromHex(fields[1])
}

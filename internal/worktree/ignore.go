package worktree

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IgnoreRule 代表单条 .gitignore 规则
type IgnoreRule struct {
	Pattern   string
	IsNegated bool
	DirOnly   bool
	BaseDir   string // 该规则所在的相对根目录（如 "" 表示根目录，"pkg" 表示 pkg/ 下的 .gitignore）
	regex     *regexp.Regexp
}

// IgnoreMatcher 负责汇总并匹配仓库内的多层级 .gitignore 规则
type IgnoreMatcher struct {
	rules []IgnoreRule
}

// NewIgnoreMatcher 创建空的忽略匹配器
func NewIgnoreMatcher() *IgnoreMatcher {
	return &IgnoreMatcher{
		rules: make([]IgnoreRule, 0),
	}
}

// LoadIgnoreFile 从指定文件加载忽略规则并追加到匹配器中
func (m *IgnoreMatcher) LoadIgnoreFile(filePath string, baseDir string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m.ParseRules(data, baseDir)
	return nil
}

// ParseRules 解析字节流中的多行 ignore 规则
func (m *IgnoreMatcher) ParseRules(data []byte, baseDir string) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		rule, ok := ParseIgnoreLine(line, baseDir)
		if ok {
			m.rules = append(m.rules, rule)
		}
	}
}

// ParseIgnoreLine 解析单行 ignore 规则
func ParseIgnoreLine(line string, baseDir string) (IgnoreRule, bool) {
	// 去除行首尾多余空格
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return IgnoreRule{}, false
	}

	isNegated := false
	if strings.HasPrefix(trimmed, "!") {
		isNegated = true
		trimmed = trimmed[1:]
	}

	dirOnly := false
	if strings.HasSuffix(trimmed, "/") {
		dirOnly = true
		trimmed = strings.TrimSuffix(trimmed, "/")
	}

	pattern := trimmed
	regex := compileGitignorePattern(pattern)

	return IgnoreRule{
		Pattern:   pattern,
		IsNegated: isNegated,
		DirOnly:   dirOnly,
		BaseDir:   baseDir,
		regex:     regex,
	}, true
}

// Match 判定指定相对路径是否被忽略。
// path 为相对于工作区根目录的正斜杠路径（如 "src/main.go"）。
// isDir 指示目标是否为目录。
func (m *IgnoreMatcher) Match(path string, isDir bool) bool {
	ignored := false
	cleanPath := filepath.ToSlash(path)

	for _, rule := range m.rules {
		if rule.DirOnly && !isDir {
			continue
		}

		// 检查路径是否位于规则所属的 BaseDir 范围内
		target := cleanPath
		if rule.BaseDir != "" {
			if !strings.HasPrefix(cleanPath, rule.BaseDir+"/") && cleanPath != rule.BaseDir {
				continue
			}
			target = strings.TrimPrefix(cleanPath, rule.BaseDir+"/")
		}

		if matchRule(rule, target, isDir) {
			ignored = !rule.IsNegated
		}
	}

	return ignored
}

func matchRule(rule IgnoreRule, target string, isDir bool) bool {
	if rule.regex == nil {
		return false
	}
	return rule.regex.MatchString(target)
}

// compileGitignorePattern 将 .gitignore 通配符语法转换为精确的 Go 正则表达式
func compileGitignorePattern(pattern string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")

	hasSlash := strings.Contains(pattern, "/")
	isAnchored := strings.HasPrefix(pattern, "/")
	if isAnchored {
		pattern = pattern[1:]
	}

	if !hasSlash {
		// 规则内无任何斜杠：可在任意子目录下匹配文件名，相当于 (?:^|.*/)pattern$
		sb.WriteString("(?:.*/)?")
	}

	// 逐字符转换通配符
	chars := []rune(pattern)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c == '*' {
			if i+1 < len(chars) && chars[i+1] == '*' {
				// 处理 "**"
				i++
				if i+1 < len(chars) && chars[i+1] == '/' {
					// "/**/" 或 "**/": 匹配零或多个目录
					sb.WriteString("(?:.*/)?")
					i++
				} else {
					// "**" 匹配任意字符
					sb.WriteString(".*")
				}
			} else {
				// 单个 "*": 匹配除 '/' 之外的任意字符
				sb.WriteString("[^/]*")
			}
		} else if c == '?' {
			// 单个 "?": 匹配除 '/' 之外的单字符
			sb.WriteString("[^/]")
		} else if c == '.' || c == '+' || c == '(' || c == ')' || c == '|' || c == '^' || c == '$' || c == '{' || c == '}' || c == '\\' {
			// 正则元字符转义
			sb.WriteRune('\\')
			sb.WriteRune(c)
		} else {
			sb.WriteRune(c)
		}
	}

	sb.WriteString("(?:/.*)?$") // 若为目录，支持匹配其下级所有文件

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil
	}
	return re
}

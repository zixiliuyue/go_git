package worktree

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AttrValue 代表属性取值状态
type AttrValue struct {
	IsSet     bool
	IsUnset   bool
	Value     string
	Specified bool
}

// AttrRule 代表单条 .gitattributes 规则
type AttrRule struct {
	Pattern string
	Attrs   map[string]AttrValue
	BaseDir string
	regex   *regexp.Regexp
}

// AttributesMatcher 管理工作区属性规则集合
type AttributesMatcher struct {
	rules []AttrRule
}

// NewAttributesMatcher 创建空的属性匹配器
func NewAttributesMatcher() *AttributesMatcher {
	return &AttributesMatcher{
		rules: make([]AttrRule, 0),
	}
}

// LoadAttributesFile 从指定路径加载 .gitattributes 文件
func (m *AttributesMatcher) LoadAttributesFile(filePath string, baseDir string) error {
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

// ParseRules 解析多行 attributes 规则
func (m *AttributesMatcher) ParseRules(data []byte, baseDir string) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pattern := fields[0]
		attrs := make(map[string]AttrValue)

		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") {
				// unset 属性
				attrs[strings.TrimPrefix(field, "-")] = AttrValue{IsUnset: true, Specified: true}
			} else if strings.HasPrefix(field, "!") {
				// uncommitted / unspecified
				attrs[strings.TrimPrefix(field, "!")] = AttrValue{Specified: false}
			} else if eqIdx := strings.Index(field, "="); eqIdx >= 0 {
				k := field[:eqIdx]
				v := field[eqIdx+1:]
				attrs[k] = AttrValue{Value: v, Specified: true}
			} else {
				// set 属性
				attrs[field] = AttrValue{IsSet: true, Specified: true}
			}
		}

		rule := AttrRule{
			Pattern: pattern,
			Attrs:   attrs,
			BaseDir: baseDir,
			regex:   compileGitignorePattern(pattern),
		}
		m.rules = append(m.rules, rule)
	}
}

// Query 查询指定相对路径的特定属性值
func (m *AttributesMatcher) Query(path string, attrName string) AttrValue {
	cleanPath := filepath.ToSlash(path)
	var res AttrValue

	for _, rule := range m.rules {
		target := cleanPath
		if rule.BaseDir != "" {
			if !strings.HasPrefix(cleanPath, rule.BaseDir+"/") && cleanPath != rule.BaseDir {
				continue
			}
			target = strings.TrimPrefix(cleanPath, rule.BaseDir+"/")
		}

		if rule.regex != nil && rule.regex.MatchString(target) {
			if val, ok := rule.Attrs[attrName]; ok {
				res = val
			}
		}
	}

	return res
}

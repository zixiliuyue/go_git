package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 表示解析后的 Git 配置文件结构（支持全局与仓库级）。
type Config struct {
	sections map[string]map[string]string
}

// NewConfig 创建新的配置对象。
func NewConfig() *Config {
	return &Config{
		sections: make(map[string]map[string]string),
	}
}

// Get 获取指定 section（支持 section 或 section.subsection）的配置项值。
func (c *Config) Get(section, key string) string {
	s := strings.ToLower(section)
	k := strings.ToLower(key)
	if sec, ok := c.sections[s]; ok {
		return sec[k]
	}
	return ""
}

// Set 设置配置项。
func (c *Config) Set(section, key, value string) {
	s := strings.ToLower(section)
	k := strings.ToLower(key)
	if _, ok := c.sections[s]; !ok {
		c.sections[s] = make(map[string]string)
	}
	c.sections[s][k] = value
}

// ParseConfigFile 从指定路径解析 Git INI 格式配置文件，支持递归 include 与 includeIf。
func ParseConfigFile(path string, gitDir string) (*Config, error) {
	cfg := NewConfig()
	if err := parseFileInto(cfg, path, gitDir, make(map[string]bool)); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	return cfg, nil
}

// parseFileInto 递归解析配置文件
func parseFileInto(cfg *Config, filePath string, gitDir string, visited map[string]bool) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	if visited[absPath] {
		return nil // 避免循环引用
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 忽略空行与注释行（# 或 ;）
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// 解析 Section，形如 [core] 或 [remote "origin"]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inner := strings.TrimSpace(line[1 : len(line)-1])
			if idx := strings.IndexByte(inner, '"'); idx > 0 {
				secName := strings.TrimSpace(inner[:idx])
				subName := strings.Trim(strings.TrimSpace(inner[idx:]), "\"")
				currentSection = fmt.Sprintf("%s.%s", secName, subName)
			} else {
				currentSection = inner
			}
			continue
		}

		// 解析 key = value
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx > 0 {
			k := strings.TrimSpace(line[:eqIdx])
			v := strings.TrimSpace(line[eqIdx+1:])
			// 去除可能的外围引号
			if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") && len(v) >= 2 {
				v = v[1 : len(v)-1]
			}
			cfg.Set(currentSection, k, v)

			// 处理 include.path 指令
			if strings.ToLower(currentSection) == "include" && strings.ToLower(k) == "path" {
				incPath := v
				if strings.HasPrefix(incPath, "~/") {
					if home, hErr := os.UserHomeDir(); hErr == nil {
						incPath = filepath.Join(home, incPath[2:])
					}
				} else if !filepath.IsAbs(incPath) {
					incPath = filepath.Join(filepath.Dir(absPath), incPath)
				}
				_ = parseFileInto(cfg, incPath, gitDir, visited)
			}
		}
	}

	return scanner.Err()
}

// Serialize 将当前配置序列化为标准 Git INI 格式文本。
func (c *Config) Serialize() []byte {
	var buf bytes.Buffer
	for secName, entries := range c.sections {
		if strings.Contains(secName, ".") {
			parts := strings.SplitN(secName, ".", 2)
			buf.WriteString(fmt.Sprintf("[%s \"%s\"]\n", parts[0], parts[1]))
		} else {
			buf.WriteString(fmt.Sprintf("[%s]\n", secName))
		}
		for k, v := range entries {
			buf.WriteString(fmt.Sprintf("\t%s = %s\n", k, v))
		}
	}
	return buf.Bytes()
}

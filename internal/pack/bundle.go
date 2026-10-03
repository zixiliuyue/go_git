package pack

import (
	"bufio"
	"bytes"
	"fmt"
	"gogit/internal/object"
	"io"
	"strings"
)

// BundleRef 表示 Bundle 文件中的引用记录或先决条件
type BundleRef struct {
	OID     object.Hash
	Name    string // 引用名（如 "refs/heads/main" 或 "HEAD"）
	Comment string // 选填注释（如先决条件的提交信息摘要）
}

// BundleHeader 封装 Git Bundle 文件头部元数据
type BundleHeader struct {
	Version       int               // 2 或 3
	Capabilities  map[string]string // v3 扩展能力（如 @object-format=sha1）
	Prerequisites []BundleRef       // 先决提交（以 '-' 开头）
	References    []BundleRef       // 包含的正向引用列表
}

// ReadBundle 从输入流中解析 Bundle 头部，并分离出随后的 Packfile 二进制流
func ReadBundle(r io.Reader) (*BundleHeader, []byte, error) {
	br := bufio.NewReader(r)

	// 1. 读取首行版本标识
	magic, err := br.ReadString('\n')
	if err != nil {
		return nil, nil, fmt.Errorf("读取 bundle 头部标识失败: %w", err)
	}
	magic = strings.TrimSpace(magic)

	header := &BundleHeader{
		Capabilities: make(map[string]string),
	}

	switch magic {
	case "# v2 git bundle":
		header.Version = 2
	case "# v3 git bundle":
		header.Version = 3
	default:
		return nil, nil, fmt.Errorf("不支持或无效的 git bundle 格式: %s", magic)
	}

	// 2. 逐行读取 Capabilities、先决条件与引用，直至遇到空行
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, nil, fmt.Errorf("解析 bundle 头部行失败: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			// 遇到空行，头部解析结束
			break
		}

		if strings.HasPrefix(trimmed, "@") {
			// v3 Capability: @key[=value]
			capStr := strings.TrimPrefix(trimmed, "@")
			parts := strings.SplitN(capStr, "=", 2)
			if len(parts) == 2 {
				header.Capabilities[parts[0]] = parts[1]
			} else {
				header.Capabilities[parts[0]] = ""
			}
			continue
		}

		if strings.HasPrefix(trimmed, "-") {
			// 先决条件: -<oid>[ <comment>]
			prereqStr := strings.TrimPrefix(trimmed, "-")
			fields := strings.Fields(prereqStr)
			if len(fields) == 0 {
				continue
			}
			h, err := object.NewHashFromHex(fields[0])
			if err != nil {
				return nil, nil, fmt.Errorf("无效的先决条件哈希 %s: %w", fields[0], err)
			}
			comment := ""
			if len(fields) > 1 {
				comment = strings.Join(fields[1:], " ")
			}
			header.Prerequisites = append(header.Prerequisites, BundleRef{
				OID:     h,
				Comment: comment,
			})
			continue
		}

		// 正向引用: <oid> <refname>
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			return nil, nil, fmt.Errorf("无效的 bundle 引用定义: %s", trimmed)
		}
		h, err := object.NewHashFromHex(fields[0])
		if err != nil {
			return nil, nil, fmt.Errorf("无效的引用哈希 %s: %w", fields[0], err)
		}
		header.References = append(header.References, BundleRef{
			OID:  h,
			Name: fields[1],
		})
	}

	// 3. 读取空行之后的所有剩余数据作为 Packfile 数据流
	packData, err := io.ReadAll(br)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 bundle packfile 流失败: %w", err)
	}

	return header, packData, nil
}

// WriteBundle 将 BundleHeader 与 packfile 数据序列化输出到 writer
func WriteBundle(w io.Writer, header *BundleHeader, packData []byte) error {
	var buf bytes.Buffer

	ver := header.Version
	if ver != 3 {
		ver = 2
	}
	buf.WriteString(fmt.Sprintf("# v%d git bundle\n", ver))

	// v3 Capabilities
	if ver == 3 {
		for k, v := range header.Capabilities {
			if v != "" {
				buf.WriteString(fmt.Sprintf("@%s=%s\n", k, v))
			} else {
				buf.WriteString(fmt.Sprintf("@%s\n", k))
			}
		}
	}

	// 先决条件
	for _, p := range header.Prerequisites {
		if p.Comment != "" {
			buf.WriteString(fmt.Sprintf("-%s %s\n", p.OID.String(), p.Comment))
		} else {
			buf.WriteString(fmt.Sprintf("-%s\n", p.OID.String()))
		}
	}

	// 引用列表
	for _, ref := range header.References {
		buf.WriteString(fmt.Sprintf("%s %s\n", ref.OID.String(), ref.Name))
	}

	// 终止空行
	buf.WriteString("\n")

	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("写入 bundle 头部失败: %w", err)
	}

	if _, err := w.Write(packData); err != nil {
		return fmt.Errorf("写入 bundle packfile 失败: %w", err)
	}

	return nil
}

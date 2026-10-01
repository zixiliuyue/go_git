package transport

import (
	"errors"
	"fmt"
	"gogit/internal/object"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ProtocolType 定义远端支持的网络协议类型
type ProtocolType string

const (
	ProtoHTTP  ProtocolType = "http"
	ProtoHTTPS ProtocolType = "https"
	ProtoSSH   ProtocolType = "ssh"
	ProtoGit   ProtocolType = "git"
	ProtoFile  ProtocolType = "file"
)

// Endpoint 描述远端 Git 仓库的网络访问端点
type Endpoint struct {
	Original string
	Protocol ProtocolType
	Host     string
	Port     string
	User     string
	Path     string
}

// ParseEndpoint 将用户输入的各种 Git URL 或本地路径解析为统一的 Endpoint 结构体
func ParseEndpoint(raw string) (*Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty repository url")
	}

	// 1. 本地绝对路径或相对路径判断
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || raw == "." {
		abs, err := filepath.Abs(raw)
		if err != nil {
			abs = raw
		}
		return &Endpoint{
			Original: raw,
			Protocol: ProtoFile,
			Path:     abs,
		}, nil
	}

	// 2. 标准 URI Scheme 解析 (http, https, git, ssh, file)
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid repository url: %w", err)
		}

		ep := &Endpoint{
			Original: raw,
			Host:     u.Hostname(),
			Port:     u.Port(),
			Path:     u.Path,
		}

		if u.User != nil {
			ep.User = u.User.Username()
		}

		switch strings.ToLower(u.Scheme) {
		case "http":
			ep.Protocol = ProtoHTTP
		case "https":
			ep.Protocol = ProtoHTTPS
		case "ssh":
			ep.Protocol = ProtoSSH
		case "git":
			ep.Protocol = ProtoGit
		case "file":
			ep.Protocol = ProtoFile
			ep.Path = u.Path
		default:
			return nil, fmt.Errorf("unsupported protocol scheme %q", u.Scheme)
		}

		return ep, nil
	}

	// 3. SCP 风格的 SSH 简写格式：user@host:path/to/repo.git 或 host:path/to/repo.git
	if atIdx := strings.Index(raw, "@"); atIdx != -1 {
		colonIdx := strings.Index(raw, ":")
		if colonIdx > atIdx {
			user := raw[:atIdx]
			host := raw[atIdx+1 : colonIdx]
			path := raw[colonIdx+1:]
			return &Endpoint{
				Original: raw,
				Protocol: ProtoSSH,
				User:     user,
				Host:     host,
				Path:     path,
			}, nil
		}
	} else if colonIdx := strings.Index(raw, ":"); colonIdx != -1 && !strings.Contains(raw, "/") {
		// host:path
		host := raw[:colonIdx]
		path := raw[colonIdx+1:]
		return &Endpoint{
			Original: raw,
			Protocol: ProtoSSH,
			Host:     host,
			Path:     path,
		}, nil
	}

	// 4. 若为存在的本地路径或目录
	if _, err := os.Stat(raw); err == nil {
		abs, _ := filepath.Abs(raw)
		return &Endpoint{
			Original: raw,
			Protocol: ProtoFile,
			Path:     abs,
		}, nil
	}

	// 默认兜底作为本地路径尝试
	abs, _ := filepath.Abs(raw)
	return &Endpoint{
		Original: raw,
		Protocol: ProtoFile,
		Path:     abs,
	}, nil
}

// RemoteRef 描述远端仓库暴露的一个引用
type RemoteRef struct {
	OID     object.Hash
	Name    string
	Target  string      // 符号引用目标（如 HEAD -> refs/heads/main）
	Peeled  object.Hash // 针对附注标签剥离后的实际 commit/blob 哈希 (^{})
}

// RemoteCapabilities 远端能力集
type RemoteCapabilities struct {
	Version        int // 1 或 2
	MultiAck       bool
	Sideband64k    bool
	OfsDelta       bool
	ThinPack       bool
	NoProgress     bool
	IncludeTag     bool
	ReportStatus   bool
	DeleteRefs     bool
	Quiet          bool
	Agent          string
	Symrefs        map[string]string // symref 映射（如 HEAD -> refs/heads/main）
	Raw            []string
}

package object

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Signature 代表 Git commit 或 tag 中的作者/提交者身份与时间戳信息。
// 格式形如: "Linus Torvalds <torvalds@linux-foundation.org> 1700000000 +0800"
type Signature struct {
	Name  string
	Email string
	When  time.Time
	TZ    string // 例如 "+0800", "-0700"
}

// String 格式化 Signature 为 Git 规范字符串。
func (s Signature) String() string {
	ts := s.When.Unix()
	tz := s.TZ
	if tz == "" {
		tz = FormatTimezone(s.When)
	}
	return fmt.Sprintf("%s <%s> %d %s", s.Name, s.Email, ts, tz)
}

// FormatTimezone 格式化 time.Time 的时区为 Git 规范偏移（如 +0800）。
func FormatTimezone(t time.Time) string {
	_, offsetSec := t.Zone()
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
}

// ParseSignature 解析 Git 规范的签名字符串。
func ParseSignature(text string) (Signature, error) {
	lt := strings.LastIndexByte(text, '<')
	gt := strings.LastIndexByte(text, '>')
	if lt < 0 || gt < 0 || lt >= gt {
		return Signature{}, fmt.Errorf("无效的签名格式: %q", text)
	}

	name := strings.TrimSpace(text[:lt])
	email := strings.TrimSpace(text[lt+1 : gt])

	rest := strings.TrimSpace(text[gt+1:])
	parts := strings.Fields(rest)
	if len(parts) < 1 {
		return Signature{}, fmt.Errorf("签名缺少时间戳: %q", text)
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Signature{}, fmt.Errorf("解析时间戳失败: %w", err)
	}

	tz := "+0000"
	if len(parts) >= 2 {
		tz = parts[1]
	}

	// 计算时区偏移构造 time.Time
	offsetSec := 0
	if len(tz) == 5 && (tz[0] == '+' || tz[0] == '-') {
		h, _ := strconv.Atoi(tz[1:3])
		m, _ := strconv.Atoi(tz[3:5])
		offsetSec = h*3600 + m*60
		if tz[0] == '-' {
			offsetSec = -offsetSec
		}
	}
	loc := time.FixedZone(tz, offsetSec)
	t := time.Unix(ts, 0).In(loc)

	return Signature{
		Name:  name,
		Email: email,
		When:  t,
		TZ:    tz,
	}, nil
}

// Commit 表示一个 Git 提交对象。
type Commit struct {
	Tree      Hash
	Parents   []Hash
	Author    Signature
	Committer Signature
	GPGSig    string
	Message   string
	raw       []byte
	hash      Hash
}

// Type 实现 Object 接口。
func (c *Commit) Type() ObjectType {
	return TypeCommit
}

// Payload 返回序列化后的 Commit 数据。
func (c *Commit) Payload() []byte {
	if c.raw == nil {
		c.raw = c.Serialize()
	}
	return c.raw
}

// Hash 返回 Commit 对象的 SHA-1。
func (c *Commit) Hash() Hash {
	if c.hash.IsZero() {
		c.hash = HashObject(TypeCommit, c.Payload())
	}
	return c.hash
}

// Serialize 将 Commit 结构体序列化为 Git 标准二进制格式。
func (c *Commit) Serialize() []byte {
	var buf bytes.Buffer
	buf.WriteString("tree ")
	buf.WriteString(c.Tree.String())
	buf.WriteByte('\n')

	for _, p := range c.Parents {
		buf.WriteString("parent ")
		buf.WriteString(p.String())
		buf.WriteByte('\n')
	}

	buf.WriteString("author ")
	buf.WriteString(c.Author.String())
	buf.WriteByte('\n')

	buf.WriteString("committer ")
	buf.WriteString(c.Committer.String())
	buf.WriteByte('\n')

	if c.GPGSig != "" {
		buf.WriteString("gpgsig ")
		// gpg 签名跨多行时，后续每行需以空格缩进
		lines := strings.Split(c.GPGSig, "\n")
		for i, line := range lines {
			if i > 0 && line != "" {
				buf.WriteByte(' ')
			}
			buf.WriteString(line)
			if i < len(lines)-1 || strings.HasSuffix(c.GPGSig, "\n") {
				buf.WriteByte('\n')
			}
		}
		if !strings.HasSuffix(c.GPGSig, "\n") {
			buf.WriteByte('\n')
		}
	}

	buf.WriteByte('\n')
	buf.WriteString(c.Message)

	return buf.Bytes()
}

// ParseCommit 解析 Git commit 的原始二进制内容。
func ParseCommit(data []byte) (*Commit, error) {
	c := &Commit{
		raw:  data,
		hash: HashObject(TypeCommit, data),
	}

	lines := bytes.Split(data, []byte{'\n'})
	var inHeader = true
	var inGPGSig = false
	var gpgSigBuf bytes.Buffer
	var msgLines [][]byte

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if inHeader {
			if len(line) == 0 {
				// 空行标志头部结束，后续全部为提交信息
				inHeader = false
				msgLines = lines[i+1:]
				break
			}

			// 如果是 gpgsig 延续行（以空格开头）
			if inGPGSig {
				if len(line) > 0 && line[0] == ' ' {
					gpgSigBuf.WriteByte('\n')
					gpgSigBuf.Write(line[1:])
					continue
				}
				// 退出 gpgsig
				inGPGSig = false
				c.GPGSig = gpgSigBuf.String()
			}

			if bytes.HasPrefix(line, []byte("tree ")) {
				hStr := string(bytes.TrimSpace(line[5:]))
				h, err := NewHashFromHex(hStr)
				if err != nil {
					return nil, fmt.Errorf("解析 commit tree 哈希失败: %w", err)
				}
				c.Tree = h
			} else if bytes.HasPrefix(line, []byte("parent ")) {
				hStr := string(bytes.TrimSpace(line[7:]))
				h, err := NewHashFromHex(hStr)
				if err != nil {
					return nil, fmt.Errorf("解析 commit parent 哈希失败: %w", err)
				}
				c.Parents = append(c.Parents, h)
			} else if bytes.HasPrefix(line, []byte("author ")) {
				sig, err := ParseSignature(string(line[7:]))
				if err != nil {
					return nil, fmt.Errorf("解析 commit author 失败: %w", err)
				}
				c.Author = sig
			} else if bytes.HasPrefix(line, []byte("committer ")) {
				sig, err := ParseSignature(string(line[10:]))
				if err != nil {
					return nil, fmt.Errorf("解析 commit committer 失败: %w", err)
				}
				c.Committer = sig
			} else if bytes.HasPrefix(line, []byte("gpgsig ")) {
				inGPGSig = true
				gpgSigBuf.Reset()
				gpgSigBuf.Write(line[7:])
			}
		}
	}

	if inGPGSig {
		c.GPGSig = gpgSigBuf.String()
	}

	if inHeader {
		return nil, errors.New("无效的 commit 格式：缺少消息体分隔空行")
	}

	c.Message = string(bytes.Join(msgLines, []byte{'\n'}))
	return c, nil
}

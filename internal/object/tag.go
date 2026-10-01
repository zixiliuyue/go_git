package object

import (
	"bytes"
	"errors"
	"fmt"
)

// Tag 表示 Git 中的附注标签对象（Annotated Tag）。
type Tag struct {
	Object     Hash
	ObjectType ObjectType
	Name       string
	Tagger     Signature
	Message    string
	raw        []byte
	hash       Hash
}

// Type 实现 Object 接口。
func (t *Tag) Type() ObjectType {
	return TypeTag
}

// Payload 返回序列化后的 Tag 内容。
func (t *Tag) Payload() []byte {
	if t.raw == nil {
		t.raw = t.Serialize()
	}
	return t.raw
}

// Hash 返回 Tag 对象的 SHA-1。
func (t *Tag) Hash() Hash {
	if t.hash.IsZero() {
		t.hash = HashObject(TypeTag, t.Payload())
	}
	return t.hash
}

// Serialize 将 Tag 结构体序列化为 Git 标准二进制格式。
func (t *Tag) Serialize() []byte {
	var buf bytes.Buffer
	buf.WriteString("object ")
	buf.WriteString(t.Object.String())
	buf.WriteByte('\n')

	buf.WriteString("type ")
	buf.WriteString(string(t.ObjectType))
	buf.WriteByte('\n')

	buf.WriteString("tag ")
	buf.WriteString(t.Name)
	buf.WriteByte('\n')

	buf.WriteString("tagger ")
	buf.WriteString(t.Tagger.String())
	buf.WriteByte('\n')

	buf.WriteByte('\n')
	buf.WriteString(t.Message)

	return buf.Bytes()
}

// ParseTag 解析 Git 附注标签的二进制内容。
func ParseTag(data []byte) (*Tag, error) {
	tag := &Tag{
		raw:  data,
		hash: HashObject(TypeTag, data),
	}

	lines := bytes.Split(data, []byte{'\n'})
	var inHeader = true
	var msgLines [][]byte

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if inHeader {
			if len(line) == 0 {
				inHeader = false
				msgLines = lines[i+1:]
				break
			}

			if bytes.HasPrefix(line, []byte("object ")) {
				hStr := string(bytes.TrimSpace(line[7:]))
				h, err := NewHashFromHex(hStr)
				if err != nil {
					return nil, fmt.Errorf("解析 tag target hash 失败: %w", err)
				}
				tag.Object = h
			} else if bytes.HasPrefix(line, []byte("type ")) {
				tag.ObjectType = ObjectType(bytes.TrimSpace(line[5:]))
			} else if bytes.HasPrefix(line, []byte("tag ")) {
				tag.Name = string(bytes.TrimSpace(line[4:]))
			} else if bytes.HasPrefix(line, []byte("tagger ")) {
				sig, err := ParseSignature(string(line[7:]))
				if err != nil {
					return nil, fmt.Errorf("解析 tagger 失败: %w", err)
				}
				tag.Tagger = sig
			}
		}
	}

	if inHeader {
		return nil, errors.New("无效的 tag 格式：缺少消息体分隔空行")
	}

	tag.Message = string(bytes.Join(msgLines, []byte{'\n'}))
	return tag, nil
}

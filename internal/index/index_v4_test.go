package index

import (
	"gogit/internal/object"
	"testing"
)

func TestIndexV4Roundtrip(t *testing.T) {
	orig := NewIndex()
	orig.Version = 4
	orig.Entries = []*IndexEntry{
		{
			Path: "a/b/c.go",
			Mode: 0100644,
			OID:  object.MustHashFromHex("1111111111111111111111111111111111111111"),
		},
		{
			Path: "a/b/d.go",
			Mode: 0100644,
			OID:  object.MustHashFromHex("2222222222222222222222222222222222222222"),
		},
		{
			Path: "z/final.go",
			Mode: 0100755,
			OID:  object.MustHashFromHex("3333333333333333333333333333333333333333"),
		},
	}

	data, err := orig.Serialize()
	if err != nil {
		t.Fatalf("Serialize v4 失败: %v", err)
	}

	parsed, err := ParseIndex(data)
	if err != nil {
		t.Fatalf("ParseIndex v4 失败: %v", err)
	}

	if parsed.Version != 4 {
		t.Fatalf("期望版本 4，实际为 %d", parsed.Version)
	}
	if len(parsed.Entries) != 3 {
		t.Fatalf("期望 3 个条目，实际为 %d", len(parsed.Entries))
	}
	for i, e := range parsed.Entries {
		if e.Path != orig.Entries[i].Path {
			t.Fatalf("条目 %d 路径不匹配: %s != %s", i, e.Path, orig.Entries[i].Path)
		}
		if e.OID != orig.Entries[i].OID {
			t.Fatalf("条目 %d OID 不匹配: %s != %s", i, e.OID.String(), orig.Entries[i].OID.String())
		}
	}
}

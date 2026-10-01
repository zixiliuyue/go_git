package diff

import (
	"strings"
	"testing"
)

func TestMyersDiff(t *testing.T) {
	a := []string{"A", "B", "C", "A", "B", "B", "A"}
	b := []string{"C", "B", "A", "B", "A", "C"}

	ops := MyersDiff(a, b)
	if len(ops) == 0 {
		t.Fatal("MyersDiff 结果不应为空")
	}

	hunks := GenerateUnifiedHunks(ops, 3)
	if len(hunks) == 0 {
		t.Fatal("生成的 hunks 不应为空")
	}

	diffText := FormatUnifiedDiff("file.old", "file.new", hunks)
	if !strings.Contains(diffText, "--- a/file.old") || !strings.Contains(diffText, "+++ b/file.new") {
		t.Fatalf("生成的 diff 缺少统一头部: %s", diffText)
	}
}

func TestFormatStat(t *testing.T) {
	diffs := []*FileDiff{
		{
			OldPath:      "main.go",
			NewPath:      "main.go",
			AddedLines:   5,
			DeletedLines: 2,
		},
	}

	stat := formatStat(diffs)
	if !strings.Contains(stat, "main.go") || !strings.Contains(stat, "1 file changed, 5 insertions(+), 2 deletions(-)") {
		t.Fatalf("stat 输出不匹配预期: %s", stat)
	}
}

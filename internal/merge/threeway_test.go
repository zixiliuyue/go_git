package merge

import (
	"strings"
	"testing"
)

func TestMerge3WayClean(t *testing.T) {
	base := []byte("line 1\nline 2\nline 3\n")
	ours := []byte("line 1 (ours)\nline 2\nline 3\n")
	theirs := []byte("line 1\nline 2\nline 3 (theirs)\n")

	res := Merge3Way(base, ours, theirs, "HEAD", "feature")
	if res.HasConflict {
		t.Fatalf("非冲突合并误报冲突: %s", string(res.Content))
	}

	expected := "line 1 (ours)\nline 2\nline 3 (theirs)\n"
	if string(res.Content) != expected {
		t.Fatalf("合并内容不符合预期:\n实际:\n%s\n期望:\n%s", string(res.Content), expected)
	}
}

func TestMerge3WayConflict(t *testing.T) {
	base := []byte("line 1\nline 2\n")
	ours := []byte("line 1\nconflict from ours\n")
	theirs := []byte("line 1\nconflict from theirs\n")

	res := Merge3Way(base, ours, theirs, "HEAD", "feature")
	if !res.HasConflict {
		t.Fatalf("冲突合并未能检测到冲突")
	}

	out := string(res.Content)
	if !strings.Contains(out, "<<<<<<< HEAD") ||
		!strings.Contains(out, "conflict from ours") ||
		!strings.Contains(out, "=======") ||
		!strings.Contains(out, "conflict from theirs") ||
		!strings.Contains(out, ">>>>>>> feature") {
		t.Fatalf("冲突标记格式不符合 Git 标准:\n%s", out)
	}
}

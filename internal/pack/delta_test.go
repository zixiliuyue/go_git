package pack

import (
	"bytes"
	"testing"
)

func TestDeltaRoundTrip(t *testing.T) {
	base := []byte("The quick brown fox jumps over the lazy dog.\nAll good men must come to the aid of their country.\nLine three with extra content.")
	target := []byte("The fast brown fox jumps over the lazy dog.\nAll good men must come to the aid of their country.\nLine three with extra content modified completely!")

	delta := CreateDelta(base, target)
	if len(delta) == 0 {
		t.Fatalf("生成的 delta 为空")
	}

	applied, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta 失败: %v", err)
	}

	if !bytes.Equal(applied, target) {
		t.Fatalf("Delta 还原内容不一致:\n期望: %s\n实际: %s", string(target), string(applied))
	}
}

package merge

import (
	"bytes"
	"fmt"
	"gogit/internal/diff"
	"strings"
)

// Merge3WayResult 保存三路文本合并的结果与冲突状态
type Merge3WayResult struct {
	Content     []byte
	HasConflict bool
}

// Merge3Way 对单文件内容执行标准 Git 三路合并算法（base / ours / theirs）。
// 生成与 Git 严格一致的冲突标记（<<<<<<< ours, =======, >>>>>>> theirs）。
func Merge3Way(base, ours, theirs []byte, oursLabel, theirsLabel string) Merge3WayResult {
	if bytes.Equal(ours, theirs) {
		// 两端修改完全相同或均未修改，直接返回
		return Merge3WayResult{Content: ours, HasConflict: false}
	}
	if bytes.Equal(base, ours) {
		// ours 未改动，直接采用 theirs
		return Merge3WayResult{Content: theirs, HasConflict: false}
	}
	if bytes.Equal(base, theirs) {
		// theirs 未改动，直接采用 ours
		return Merge3WayResult{Content: ours, HasConflict: false}
	}

	baseLines := splitLinesWithNewlines(string(base))
	oursLines := splitLinesWithNewlines(string(ours))
	theirsLines := splitLinesWithNewlines(string(theirs))

	// 使用 Myers diff 分别计算 base -> ours 与 base -> theirs 的编辑操作
	oursOps := diff.MyersDiff(cleanLines(baseLines), cleanLines(oursLines))
	theirsOps := diff.MyersDiff(cleanLines(baseLines), cleanLines(theirsLines))

	// 将编辑操作转换为区间映射
	oursHunks := opsToHunks(oursOps, baseLines, oursLines)
	theirsHunks := opsToHunks(theirsOps, baseLines, theirsLines)

	// 合并两个 hunk 序列
	merged, hasConflict := mergeHunkStreams(baseLines, oursHunks, theirsHunks, oursLabel, theirsLabel)

	return Merge3WayResult{
		Content:     []byte(strings.Join(merged, "")),
		HasConflict: hasConflict,
	}
}

type diffHunk struct {
	baseStart int // 在 base 中的起始行 (0-indexed)
	baseCount int // 消耗 base 的行数
	newLines  []string
}

func opsToHunks(ops []diff.EditOp, baseLines, newLines []string) []diffHunk {
	var hunks []diffHunk
	baseIdx := 0
	newIdx := 0

	i := 0
	for i < len(ops) {
		if ops[i].Type == diff.OpEqual {
			baseIdx++
			newIdx++
			i++
			continue
		}

		startBase := baseIdx
		bCount := 0
		var chunk []string

		for i < len(ops) && ops[i].Type != diff.OpEqual {
			if ops[i].Type == diff.OpDelete {
				bCount++
				baseIdx++
			} else if ops[i].Type == diff.OpInsert {
				if newIdx < len(newLines) {
					chunk = append(chunk, newLines[newIdx])
				}
				newIdx++
			}
			i++
		}

		hunks = append(hunks, diffHunk{
			baseStart: startBase,
			baseCount: bCount,
			newLines:  chunk,
		})
	}

	return hunks
}

func mergeHunkStreams(baseLines []string, oursHunks, theirsHunks []diffHunk, oursLabel, theirsLabel string) ([]string, bool) {
	var out []string
	hasConflict := false
	baseLen := len(baseLines)
	baseCur := 0
	oIdx := 0
	tIdx := 0

	for baseCur < baseLen || oIdx < len(oursHunks) || tIdx < len(theirsHunks) {
		var nextO *diffHunk
		if oIdx < len(oursHunks) {
			nextO = &oursHunks[oIdx]
		}
		var nextT *diffHunk
		if tIdx < len(theirsHunks) {
			nextT = &theirsHunks[tIdx]
		}

		if nextO == nil && nextT == nil {
			if baseCur < baseLen {
				out = append(out, baseLines[baseCur:]...)
				baseCur = baseLen
			}
			break
		}

		// 确定下一个最近的变动起点
		nextStart := baseLen
		if nextO != nil && nextO.baseStart < nextStart {
			nextStart = nextO.baseStart
		}
		if nextT != nil && nextT.baseStart < nextStart {
			nextStart = nextT.baseStart
		}

		// 输出变动前未修改的 base 行
		if nextStart > baseCur {
			out = append(out, baseLines[baseCur:nextStart]...)
			baseCur = nextStart
		}

		// 检查是否有重叠冲突
		if nextO != nil && nextT != nil {
			oEnd := nextO.baseStart + nextO.baseCount
			tEnd := nextT.baseStart + nextT.baseCount

			if (nextO.baseStart < tEnd && oEnd > nextT.baseStart) || (nextO.baseStart == nextT.baseStart && nextO.baseCount == 0 && nextT.baseCount == 0) {
				// 两个分支修改了相同或相邻重叠的 base 区域
				if equalStringSlices(nextO.newLines, nextT.newLines) {
					// 两边修改一致，直接采用，无冲突
					out = append(out, nextO.newLines...)
				} else {
					// 发生冲突！输出标准冲突标记
					hasConflict = true
					out = append(out, fmt.Sprintf("<<<<<<< %s\n", oursLabel))
					out = append(out, nextO.newLines...)
					out = append(out, "=======\n")
					out = append(out, nextT.newLines...)
					out = append(out, fmt.Sprintf(">>>>>>> %s\n", theirsLabel))
				}

				maxEnd := oEnd
				if tEnd > maxEnd {
					maxEnd = tEnd
				}
				baseCur = maxEnd
				oIdx++
				tIdx++
				continue
			}
		}

		// 非重叠：优先处理更早出现的 hunk
		if nextO != nil && (nextT == nil || nextO.baseStart < nextT.baseStart) {
			out = append(out, nextO.newLines...)
			baseCur = nextO.baseStart + nextO.baseCount
			oIdx++
		} else if nextT != nil {
			out = append(out, nextT.newLines...)
			baseCur = nextT.baseStart + nextT.baseCount
			tIdx++
		}
	}

	return out, hasConflict
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func splitLinesWithNewlines(s string) []string {
	if len(s) == 0 {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func cleanLines(lines []string) []string {
	res := make([]string, len(lines))
	for i, l := range lines {
		res[i] = strings.TrimRight(l, "\r\n")
	}
	return res
}

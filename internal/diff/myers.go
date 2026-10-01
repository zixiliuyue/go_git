package diff

import (
	"fmt"
	"strings"
)

// OpType 表示 diff 编辑操作类型
type OpType int

const (
	OpEqual OpType = iota
	OpInsert
	OpDelete
)

// EditOp 表示单个编辑操作
type EditOp struct {
	Type OpType
	Line string
}

// MyersDiff 使用经典的 Myers 差分算法计算两个字符串序列的最小编辑脚本 (SES)
func MyersDiff(a, b []string) []EditOp {
	n := len(a)
	m := len(b)
	max := n + m
	if max == 0 {
		return nil
	}

	v := make(map[int]int)
	v[1] = 0

	trace := make([]map[int]int, 0)

	found := false
	for d := 0; d <= max; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1] < v[k+1]) {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k

			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}

		vCopy := make(map[int]int, len(v))
		for k, val := range v {
			vCopy[k] = val
		}
		trace = append(trace, vCopy)

		if found {
			break
		}
	}

	// 沿 trace 反向回溯还原编辑路径
	var ops []EditOp
	x := n
	y := m

	for d := len(trace) - 1; d > 0; d-- {
		vPrev := trace[d-1]
		k := x - y

		var prevK int
		if k == -d || (k != d && vPrev[k-1] < vPrev[k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := vPrev[prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY && x > 0 && y > 0 && a[x-1] == b[y-1] {
			ops = append(ops, EditOp{Type: OpEqual, Line: a[x-1]})
			x--
			y--
		}

		if d > 0 {
			if x > prevX {
				ops = append(ops, EditOp{Type: OpDelete, Line: a[x-1]})
				x--
			} else if y > prevY {
				ops = append(ops, EditOp{Type: OpInsert, Line: b[y-1]})
				y--
			}
		}
	}

	for x > 0 && y > 0 {
		ops = append(ops, EditOp{Type: OpEqual, Line: a[x-1]})
		x--
		y--
	}
	for x > 0 {
		ops = append(ops, EditOp{Type: OpDelete, Line: a[x-1]})
		x--
	}
	for y > 0 {
		ops = append(ops, EditOp{Type: OpInsert, Line: b[y-1]})
		y--
	}

	// 反转切片得到正向顺序
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	return ops
}

// Hunk 代表 Unified Diff 中的一个变动块
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Ops      []EditOp
}

// GenerateUnifiedHunks 将 EditOp 序列按指定上下文行数（默认为 3）聚合成 Hunks
func GenerateUnifiedHunks(ops []EditOp, contextLen int) []Hunk {
	if contextLen <= 0 {
		contextLen = 3
	}

	// 标记哪些 op 属于变动行以及上下文覆盖范围
	hasDiff := false
	for _, op := range ops {
		if op.Type != OpEqual {
			hasDiff = true
			break
		}
	}
	if !hasDiff {
		return nil
	}

	type annotatedOp struct {
		op      EditOp
		oldLine int
		newLine int
	}

	var annotated []annotatedOp
	oldLine := 1
	newLine := 1
	for _, op := range ops {
		annotated = append(annotated, annotatedOp{
			op:      op,
			oldLine: oldLine,
			newLine: newLine,
		})
		if op.Type == OpEqual {
			oldLine++
			newLine++
		} else if op.Type == OpDelete {
			oldLine++
		} else if op.Type == OpInsert {
			newLine++
		}
	}

	// 识别所有变动区间的起止索引并聚合上下文
	var hunks []Hunk
	n := len(annotated)
	i := 0

	for i < n {
		if annotated[i].op.Type == OpEqual {
			i++
			continue
		}

		// 找到变动起始，包含最多 contextLen 行的前置上下文
		start := i - contextLen
		if start < 0 {
			start = 0
		}

		// 寻找该 hunk 的结束位置，若相邻变动相距不超过 2*contextLen 则合并
		end := i
		for end < n {
			if annotated[end].op.Type != OpEqual {
				end++
			} else {
				// 检查后续 contextLen*2 范围内是否有新的变动
				nextDiff := -1
				for k := end; k < n && k <= end+2*contextLen; k++ {
					if annotated[k].op.Type != OpEqual {
						nextDiff = k
						break
					}
				}
				if nextDiff >= 0 {
					end = nextDiff
				} else {
					break
				}
			}
		}

		hunkEnd := end + contextLen
		if hunkEnd > n {
			hunkEnd = n
		}

		// 构建 Hunk
		var hunkOps []EditOp
		oldCount := 0
		newCount := 0
		hunkOldStart := annotated[start].oldLine
		hunkNewStart := annotated[start].newLine

		for k := start; k < hunkEnd; k++ {
			op := annotated[k].op
			hunkOps = append(hunkOps, op)
			if op.Type == OpEqual {
				oldCount++
				newCount++
			} else if op.Type == OpDelete {
				oldCount++
			} else if op.Type == OpInsert {
				newCount++
			}
		}

		if oldCount == 0 {
			hunkOldStart = 0
		}
		if newCount == 0 {
			hunkNewStart = 0
		}

		hunks = append(hunks, Hunk{
			OldStart: hunkOldStart,
			OldCount: oldCount,
			NewStart: hunkNewStart,
			NewCount: newCount,
			Ops:      hunkOps,
		})

		i = hunkEnd
	}

	return hunks
}

// formatHunkRange 根据 Git 标准格式化 Hunk 区间（当 count 为 1 时省略逗号与长度）
func formatHunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// FormatUnifiedDiff 输出标准的 Unified Diff 字符串
func FormatUnifiedDiff(oldPath, newPath string, hunks []Hunk) string {
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", oldPath))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", newPath))

	for _, h := range hunks {
		sb.WriteString(fmt.Sprintf("@@ -%s +%s @@\n", formatHunkRange(h.OldStart, h.OldCount), formatHunkRange(h.NewStart, h.NewCount)))
		for _, op := range h.Ops {
			switch op.Type {
			case OpEqual:
				sb.WriteString(" " + op.Line + "\n")
			case OpDelete:
				sb.WriteString("-" + op.Line + "\n")
			case OpInsert:
				sb.WriteString("+" + op.Line + "\n")
			}
		}
	}

	return sb.String()
}

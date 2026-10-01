package diff

import (
	"fmt"
	"strings"
)

// FormatDiff 根据用户指定的 CLI 参数格式化 diff 结果
func FormatDiff(diffs []*FileDiff, opts DiffOptions) string {
	if len(diffs) == 0 {
		return ""
	}

	if opts.NameOnly {
		var sb strings.Builder
		for _, d := range diffs {
			p := d.NewPath
			if p == "" {
				p = d.OldPath
			}
			sb.WriteString(p + "\n")
		}
		return sb.String()
	}

	if opts.NameStatus {
		var sb strings.Builder
		for _, d := range diffs {
			if d.Status == StatusRenamed {
				sb.WriteString(fmt.Sprintf("R%03d\t%s\t%s\n", d.Similarity, d.OldPath, d.NewPath))
			} else {
				p := d.NewPath
				if p == "" {
					p = d.OldPath
				}
				sb.WriteString(fmt.Sprintf("%c\t%s\n", d.Status, p))
			}
		}
		return sb.String()
	}

	if opts.NumStat {
		var sb strings.Builder
		for _, d := range diffs {
			p := d.NewPath
			if p == "" {
				p = d.OldPath
			}
			if d.IsBinary {
				sb.WriteString(fmt.Sprintf("-\t-\t%s\n", p))
			} else {
				sb.WriteString(fmt.Sprintf("%d\t%d\t%s\n", d.AddedLines, d.DeletedLines, p))
			}
		}
		return sb.String()
	}

	if opts.Stat {
		return formatStat(diffs)
	}

	// 默认 Unified Diff 完整输出
	var sb strings.Builder
	for _, d := range diffs {
		sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", d.OldPath, d.NewPath))

		if d.Status == StatusAdded {
			sb.WriteString(fmt.Sprintf("new file mode %06o\n", d.NewMode))
		} else if d.Status == StatusDeleted {
			sb.WriteString(fmt.Sprintf("deleted file mode %06o\n", d.OldMode))
		}

		oldHex := "0000000"
		if !d.OldOID.IsZero() {
			oldHex = d.OldOID.String()[:7]
		}
		newHex := "0000000"
		if !d.NewOID.IsZero() {
			newHex = d.NewOID.String()[:7]
		}
		mode := d.NewMode
		if mode == 0 {
			mode = d.OldMode
		}
		sb.WriteString(fmt.Sprintf("index %s..%s %06o\n", oldHex, newHex, mode))

		if d.IsBinary {
			sb.WriteString(fmt.Sprintf("Binary files a/%s and b/%s differ\n", d.OldPath, d.NewPath))
			continue
		}

		oldPathLabel := d.OldPath
		newPathLabel := d.NewPath
		if d.Status == StatusAdded {
			oldPathLabel = "/dev/null"
		} else {
			oldPathLabel = "a/" + oldPathLabel
		}
		if d.Status == StatusDeleted {
			newPathLabel = "/dev/null"
		} else {
			newPathLabel = "b/" + newPathLabel
		}

		sb.WriteString(fmt.Sprintf("--- %s\n", oldPathLabel))
		sb.WriteString(fmt.Sprintf("+++ %s\n", newPathLabel))

		for _, h := range d.Hunks {
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
	}

	return sb.String()
}

func formatStat(diffs []*FileDiff) string {
	var sb strings.Builder
	maxPathLen := 0
	totalAdded := 0
	totalDeleted := 0

	for _, d := range diffs {
		p := d.NewPath
		if p == "" {
			p = d.OldPath
		}
		if len(p) > maxPathLen {
			maxPathLen = len(p)
		}
		totalAdded += d.AddedLines
		totalDeleted += d.DeletedLines
	}

	for _, d := range diffs {
		p := d.NewPath
		if p == "" {
			p = d.OldPath
		}
		pad := strings.Repeat(" ", maxPathLen-len(p))
		totalChanges := d.AddedLines + d.DeletedLines
		plusStr := strings.Repeat("+", d.AddedLines)
		minusStr := strings.Repeat("-", d.DeletedLines)
		sb.WriteString(fmt.Sprintf(" %s%s | %d %s%s\n", p, pad, totalChanges, plusStr, minusStr))
	}

	fileWord := "files"
	if len(diffs) == 1 {
		fileWord = "file"
	}
	sb.WriteString(fmt.Sprintf(" %d %s changed, %d insertions(+), %d deletions(-)\n", len(diffs), fileWord, totalAdded, totalDeleted))
	return sb.String()
}

package rev

import (
	"fmt"
	"gogit/internal/object"
	"strings"
)

// GraphRow 表示 graph 输出的一行结构
type GraphRow struct {
	Prefix  string
	Item    *CommitItem
	IsMerge bool
}

// BuildCommitGraph 计算提交历史的 ASCII 分支拓扑图前缀
func BuildCommitGraph(commits []*CommitItem) []GraphRow {
	var rows []GraphRow
	columns := make([]object.Hash, 0)

	for _, item := range commits {
		c := item.Commit
		// 寻找当前 commit 在列中的位置
		colIdx := -1
		for i, h := range columns {
			if h == item.Hash {
				colIdx = i
				break
			}
		}

		if colIdx == -1 {
			// 新起点，分配在最右侧
			colIdx = len(columns)
			columns = append(columns, item.Hash)
		}

		// 构建主提交行前缀，例如 "* " 或 "| * "
		var prefix strings.Builder
		for i := 0; i < len(columns); i++ {
			if i == colIdx {
				prefix.WriteString("* ")
			} else {
				prefix.WriteString("| ")
			}
		}

		rows = append(rows, GraphRow{
			Prefix:  prefix.String(),
			Item:    item,
			IsMerge: len(c.Parents) > 1,
		})

		// 更新列状态为父提交
		if len(c.Parents) == 0 {
			// root commit，移除该列
			columns = append(columns[:colIdx], columns[colIdx+1:]...)
		} else if len(c.Parents) == 1 {
			// 单亲提交，列位置继承
			columns[colIdx] = c.Parents[0]
		} else {
			// 合并提交，第一父提交占用当前列，其余父提交插入新列
			columns[colIdx] = c.Parents[0]
			for pIdx := 1; pIdx < len(c.Parents); pIdx++ {
				// 插入新分支列
				pHash := c.Parents[pIdx]
				columns = append(columns[:colIdx+pIdx], append([]object.Hash{pHash}, columns[colIdx+pIdx:]...)...)
			}
		}
	}

	return rows
}

// FormatGraphLine 格式化带图形符号的单行日志
func FormatGraphLine(prefix string, item *CommitItem, oneline bool) string {
	c := item.Commit
	shortHex := item.Hash.String()[:7]
	firstLine := strings.TrimSpace(strings.Split(c.Message, "\n")[0])

	if oneline {
		return fmt.Sprintf("%s%s %s", prefix, shortHex, firstLine)
	}
	return fmt.Sprintf("%scommit %s\n%sAuthor: %s <%s>\n%s    %s", prefix, item.Hash.String(), prefix, c.Author.Name, c.Author.Email, prefix, firstLine)
}

package history

import (
	"fmt"
	"gogit/internal/diff"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"strings"
	"time"
)

type BlameLine struct {
	LineNum    int
	CommitOID  object.Hash
	Author     string
	AuthorTime time.Time
	Content    string
}

// Blame 计算指定文件逐行的提交归属与作者信息
func Blame(r *repo.Repository, filePath string, commitStr string, startLine, endLine int) ([]BlameLine, error) {
	targetOID, err := r.Refs.ResolveHEAD()
	if commitStr != "" {
		targetOID, err = rev.ParseRevision(r, commitStr)
	}
	if err != nil {
		return nil, fmt.Errorf("解析提交失败: %w", err)
	}

	// 1. 获取目标提交中该文件的内容
	fileContent, err := getFileContentAtCommit(r, targetOID, filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件 %s 失败: %w", filePath, err)
	}

	lines := splitLines(fileContent)
	if len(lines) == 0 {
		return nil, nil
	}

	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 || endLine > len(lines) {
		endLine = len(lines)
	}

	// 初始化每一行的当前候选归属提交
	type lineOrigin struct {
		commitOID object.Hash
		origLine  int
	}

	origins := make([]lineOrigin, len(lines))
	for i := range origins {
		origins[i] = lineOrigin{
			commitOID: targetOID,
			origLine:  i,
		}
	}

	assigned := make([]*BlameLine, len(lines))

	// 拓扑/BFS 回溯提交历史追溯每行的修改来源
	queue := []object.Hash{targetOID}
	visited := make(map[object.Hash]bool)

	for len(queue) > 0 {
		currOID := queue[0]
		queue = queue[1:]

		if visited[currOID] {
			continue
		}
		visited[currOID] = true

		currCommit, err := r.ReadCommit(currOID)
		if err != nil {
			continue
		}

		currContent, _ := getFileContentAtCommit(r, currOID, filePath)
		currLines := splitLines(currContent)

		if len(currCommit.Parents) == 0 {
			// 根提交：所有未决归属该提交的行直接定稿
			for i, o := range origins {
				if assigned[i] == nil && o.commitOID == currOID {
					assigned[i] = &BlameLine{
						LineNum:    i + 1,
						CommitOID:  currOID,
						Author:     currCommit.Author.Name,
						AuthorTime: currCommit.Author.When,
						Content:    lines[i],
					}
				}
			}
			continue
		}

		parentOID := currCommit.Parents[0]
		parentContent, _ := getFileContentAtCommit(r, parentOID, filePath)
		parentLines := splitLines(parentContent)

		// 差分 parent -> curr
		ops := diff.MyersDiff(parentLines, currLines)

		// 建立 parentLines -> currLines 的行映射
		pIdx := 0
		cIdx := 0
		currToParent := make(map[int]int)

		for _, op := range ops {
			switch op.Type {
			case diff.OpEqual:
				currToParent[cIdx] = pIdx
				pIdx++
				cIdx++
			case diff.OpDelete:
				pIdx++
			case diff.OpInsert:
				// 该行是 curr 引入的！
				cIdx++
			}
		}

		for i, o := range origins {
			if assigned[i] != nil || o.commitOID != currOID {
				continue
			}

			if pLine, ok := currToParent[o.origLine]; ok {
				// 该行在父提交中已存在，传递归属至父提交
				origins[i].commitOID = parentOID
				origins[i].origLine = pLine
			} else {
				// 该行由当前提交新增或修改，归属于 currOID
				assigned[i] = &BlameLine{
					LineNum:    i + 1,
					CommitOID:  currOID,
					Author:     currCommit.Author.Name,
					AuthorTime: currCommit.Author.When,
					Content:    lines[i],
				}
			}
		}

		if !visited[parentOID] {
			queue = append(queue, parentOID)
		}
	}

	// 最终装配结果
	var result []BlameLine
	for i := startLine - 1; i < endLine && i < len(assigned); i++ {
		if assigned[i] != nil {
			result = append(result, *assigned[i])
		} else {
			result = append(result, BlameLine{
				LineNum:   i + 1,
				CommitOID: targetOID,
				Author:    "Unknown",
				Content:   lines[i],
			})
		}
	}

	return result, nil
}

func getFileContentAtCommit(r *repo.Repository, commitOID object.Hash, filePath string) (string, error) {
	c, err := r.ReadCommit(commitOID)
	if err != nil {
		return "", err
	}
	tree, err := r.ReadTree(c.Tree)
	if err != nil {
		return "", err
	}

	parts := strings.Split(filePath, "/")
	currTree := tree

	for i, part := range parts {
		found := false
		for _, e := range currTree.Entries {
			if e.Name == part {
				found = true
				if i == len(parts)-1 {
					// 目标 Blob
					raw, err := r.ReadObject(e.OID)
					if err != nil {
						return "", err
					}
					return string(raw.Content), nil
				}
				// 子目录 Tree
				subTree, err := r.ReadTree(e.OID)
				if err != nil {
					return "", err
				}
				currTree = subTree
				break
			}
		}
		if !found {
			return "", fmt.Errorf("文件 %s 在提交中不存在", filePath)
		}
	}

	return "", fmt.Errorf("未能解析文件")
}

func splitLines(s string) []string {
	if len(s) == 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

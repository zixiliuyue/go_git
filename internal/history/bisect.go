package history

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/worktree"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BisectResult struct {
	Finished      bool
	BadCommit     object.Hash
	CurrentCommit object.Hash
	Remaining     int
	Message       string
}

// BisectStart 初始化二分查找流程
func BisectStart(r *repo.Repository, badStr, goodStr string) error {
	headRef, _ := r.Refs.ReadHEAD()
	startRef := "HEAD"
	if headRef != nil && headRef.IsSymref {
		startRef = headRef.Target
	}

	startFile := filepath.Join(r.GitDir, "BISECT_START")
	_ = os.WriteFile(startFile, []byte(startRef+"\n"), 0644)

	logFile := filepath.Join(r.GitDir, "BISECT_LOG")
	_ = os.WriteFile(logFile, []byte("# status: waiting for both good and bad commits\n"), 0644)

	if badStr != "" {
		if _, err := BisectMark(r, "bad", badStr); err != nil {
			return err
		}
	}
	if goodStr != "" {
		if _, err := BisectMark(r, "good", goodStr); err != nil {
			return err
		}
	}

	return nil
}

// BisectMark 标记提交为 good 或 bad，并推进二分步进
func BisectMark(r *repo.Repository, status, commitStr string) (*BisectResult, error) {
	targetOID, err := r.Refs.ResolveHEAD()
	if commitStr != "" {
		targetOID, err = rev.ParseRevision(r, commitStr)
	}
	if err != nil {
		return nil, fmt.Errorf("解析提交 %s 失败: %w", commitStr, err)
	}

	bisectDir := filepath.Join(r.GitDir, "refs", "bisect")
	_ = os.MkdirAll(bisectDir, 0755)

	if status == "bad" {
		badRef := filepath.Join(bisectDir, "bad")
		_ = os.WriteFile(badRef, []byte(targetOID.String()+"\n"), 0644)
	} else if status == "good" {
		goodRef := filepath.Join(bisectDir, fmt.Sprintf("good-%s", targetOID.String()))
		_ = os.WriteFile(goodRef, []byte(targetOID.String()+"\n"), 0644)
	}

	logFile := filepath.Join(r.GitDir, "BISECT_LOG")
	_ = appendToFile(logFile, fmt.Sprintf("git bisect %s %s\n", status, targetOID.String()))

	return BisectStep(r)
}

// BisectStep 计算下一个二分中点或报告首个坏提交
func BisectStep(r *repo.Repository) (*BisectResult, error) {
	bisectDir := filepath.Join(r.GitDir, "refs", "bisect")
	badBytes, err := os.ReadFile(filepath.Join(bisectDir, "bad"))
	if err != nil {
		return &BisectResult{Message: "waiting for bad commit"}, nil
	}
	badOID, err := object.NewHashFromHex(strings.TrimSpace(string(badBytes)))
	if err != nil {
		return nil, err
	}

	var goodOIDs []object.Hash
	entries, _ := os.ReadDir(bisectDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "good-") {
			data, err := os.ReadFile(filepath.Join(bisectDir, entry.Name()))
			if err == nil {
				if h, err := object.NewHashFromHex(strings.TrimSpace(string(data))); err == nil {
					goodOIDs = append(goodOIDs, h)
				}
			}
		}
	}

	if len(goodOIDs) == 0 {
		return &BisectResult{Message: "waiting for good commit"}, nil
	}

	// 查找在 bad 可达但在所有 good 不可达的提交集合
	candidates, err := rev.RevList(r, []object.Hash{badOID}, goodOIDs, rev.RevListOptions{})
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有剩余候选提交，检查 good/bad 范围是否设置正确")
	}

	if len(candidates) == 1 {
		// 定位到唯一的首个引入问题的坏提交！
		culprit := candidates[0].Hash
		cObj := candidates[0].Commit
		subject := strings.TrimSpace(strings.Split(cObj.Message, "\n")[0])
		msg := fmt.Sprintf("%s is the first bad commit\ncommit %s\nAuthor: %s <%s>\nDate:   %s\n\n    %s",
			culprit.String(), culprit.String(), cObj.Author.Name, cObj.Author.Email, cObj.Author.When.Format("Mon Jan 2 15:04:05 2006 -0700"), subject)

		return &BisectResult{
			Finished:  true,
			BadCommit: culprit,
			Message:   msg,
		}, nil
	}

	// 选取中点提交
	midIdx := len(candidates) / 2
	midCommit := candidates[midIdx]

	// 切换工作区至中点提交
	if err := worktree.CheckoutSwitch(r, midCommit.Hash.String(), worktree.CheckoutOptions{}); err != nil {
		return nil, fmt.Errorf("检出中点提交失败: %w", err)
	}

	stepsLeft := int(math.Ceil(math.Log2(float64(len(candidates)))))
	subject := strings.TrimSpace(strings.Split(midCommit.Commit.Message, "\n")[0])
	msg := fmt.Sprintf("Bisecting: %d revisions left to test after this (roughly %d steps)\n[%s] %s",
		len(candidates)/2, stepsLeft, midCommit.Hash.String()[:7], subject)

	return &BisectResult{
		Finished:      false,
		CurrentCommit: midCommit.Hash,
		Remaining:     len(candidates) / 2,
		Message:       msg,
	}, nil
}

// BisectReset 结束二分查找并恢复初始分支
func BisectReset(r *repo.Repository, target string) error {
	startFile := filepath.Join(r.GitDir, "BISECT_START")
	origBytes, err := os.ReadFile(startFile)
	origRef := "HEAD"
	if err == nil {
		origRef = strings.TrimSpace(string(origBytes))
	}

	if target != "" {
		origRef = target
	}

	_ = worktree.CheckoutSwitch(r, origRef, worktree.CheckoutOptions{})

	_ = os.Remove(startFile)
	_ = os.Remove(filepath.Join(r.GitDir, "BISECT_LOG"))
	_ = os.RemoveAll(filepath.Join(r.GitDir, "refs", "bisect"))

	return nil
}

// BisectRun 自动执行脚本判定并推进二分
func BisectRun(r *repo.Repository, cmdName string, cmdArgs []string) (*BisectResult, error) {
	for {
		stepRes, err := BisectStep(r)
		if err != nil {
			return nil, err
		}
		if stepRes.Finished {
			return stepRes, nil
		}

		c := exec.Command(cmdName, cmdArgs...)
		c.Dir = r.WorkTree
		err = c.Run()

		var markErr error
		if err == nil {
			stepRes, markErr = BisectMark(r, "good", "")
		} else {
			stepRes, markErr = BisectMark(r, "bad", "")
		}
		if markErr != nil {
			return nil, markErr
		}
		if stepRes.Finished {
			return stepRes, nil
		}
	}
}

func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

package differential

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ActionType 差分模糊测试中生成的原子操作类型
type ActionType string

const (
	ActionCreateFile ActionType = "CREATE_FILE"
	ActionModifyFile ActionType = "MODIFY_FILE"
	ActionDeleteFile ActionType = "DELETE_FILE"
	ActionRenameFile ActionType = "RENAME_FILE"
	ActionGitAdd     ActionType = "GIT_ADD"
	ActionGitCommit  ActionType = "GIT_COMMIT"
	ActionGitBranch  ActionType = "GIT_BRANCH"
	ActionGitCheckout ActionType = "GIT_CHECKOUT"
	ActionGitMerge   ActionType = "GIT_MERGE"
	ActionGitTag     ActionType = "GIT_TAG"
)

// Action 表示生成的单一可执行动作
type Action struct {
	Type        ActionType
	Path        string
	OldPath     string
	Content     []byte
	Branch      string
	Tag         string
	CommitMsg   string
	Timestamp   int64
}

// DifferentialEngine 基于属性的差分生成测试预言机
type DifferentialEngine struct {
	t            *testing.T
	rng          *rand.Rand
	seed         int64
	gogitBin     string
	dirGogit     string
	dirNativeGit string
	activeFiles  map[string]bool
	branches     []string
	tags         []string
	history      []string
	stepCounter  int
}

// NewDifferentialEngine 初始化差分测试引擎，建立双轨隔离仓库
func NewDifferentialEngine(t *testing.T, seed int64, gogitBin string) *DifferentialEngine {
	tempBase, err := os.MkdirTemp("", fmt.Sprintf("gogit-difftest-%d-*", seed))
	if err != nil {
		t.Fatalf("创建临时测试目录失败: %v", err)
	}
	tempBase, _ = filepath.EvalSymlinks(tempBase)

	dirGogit := filepath.Join(tempBase, "repo_gogit")
	dirNativeGit := filepath.Join(tempBase, "repo_native")

	if err := os.MkdirAll(dirGogit, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirNativeGit, 0755); err != nil {
		t.Fatal(err)
	}

	eng := &DifferentialEngine{
		t:            t,
		rng:          rand.New(rand.NewSource(seed)),
		seed:         seed,
		gogitBin:     gogitBin,
		dirGogit:     dirGogit,
		dirNativeGit: dirNativeGit,
		activeFiles:  make(map[string]bool),
		branches:     []string{"main"},
		tags:         []string{},
		history:      make([]string, 0),
		stepCounter:  0,
	}

	// 1. 在两边分别执行 init 初始化主分支
	eng.runBothCLI("init", "-b", "main")

	// 2. 统一配置双边 user 与 excludesfile，防止受宿主机全局 git 配置污染
	eng.runNativeGit("config", "user.name", "DiffBot")
	eng.runNativeGit("config", "user.email", "diffbot@google.com")
	eng.runNativeGit("config", "core.excludesfile", "")

	eng.runGogit("config", "user.name", "DiffBot")
	eng.runGogit("config", "user.email", "diffbot@google.com")

	return eng
}

// Cleanup 清理临时目录
func (e *DifferentialEngine) Cleanup() {
	_ = os.RemoveAll(filepath.Dir(e.dirGogit))
}

// GenerateRandomAction 属性生成器：根据当前工作区状态加权随机生成合法操作
func (e *DifferentialEngine) GenerateRandomAction() Action {
	e.stepCounter++
	now := int64(1700000000 + e.stepCounter*60)

	// 权重分配：有活动文件时增加修改与提交概率
	fileCount := len(e.activeFiles)

	weights := []struct {
		actionType ActionType
		weight     int
	}{
		{ActionCreateFile, 30},
		{ActionModifyFile, 25},
		{ActionDeleteFile, 10},
		{ActionRenameFile, 10},
		{ActionGitAdd, 20},
		{ActionGitCommit, 20},
		{ActionGitBranch, 10},
		{ActionGitCheckout, 10},
		{ActionGitTag, 5},
	}

	// 若当前无文件，强制创建文件
	if fileCount == 0 {
		return Action{
			Type:      ActionCreateFile,
			Path:      e.randomFilePath(),
			Content:   e.randomContent(),
			Timestamp: now,
		}
	}

	// 抽样选择操作类型
	totalWeight := 0
	for _, w := range weights {
		totalWeight += w.weight
	}
	choice := e.rng.Intn(totalWeight)
	accum := 0
	var selectedType ActionType
	for _, w := range weights {
		accum += w.weight
		if choice < accum {
			selectedType = w.actionType
			break
		}
	}

	switch selectedType {
	case ActionCreateFile:
		return Action{
			Type:      ActionCreateFile,
			Path:      e.randomFilePath(),
			Content:   e.randomContent(),
			Timestamp: now,
		}
	case ActionModifyFile:
		path := e.pickActiveFile()
		return Action{
			Type:      ActionModifyFile,
			Path:      path,
			Content:   e.randomContent(),
			Timestamp: now,
		}
	case ActionDeleteFile:
		path := e.pickActiveFile()
		return Action{
			Type:      ActionDeleteFile,
			Path:      path,
			Timestamp: now,
		}
	case ActionRenameFile:
		oldPath := e.pickActiveFile()
		newPath := e.randomFilePath()
		for newPath == oldPath {
			newPath = e.randomFilePath()
		}
		return Action{
			Type:      ActionRenameFile,
			OldPath:   oldPath,
			Path:      newPath,
			Timestamp: now,
		}
	case ActionGitAdd:
		return Action{
			Type:      ActionGitAdd,
			Path:      ".",
			Timestamp: now,
		}
	case ActionGitCommit:
		return Action{
			Type:      ActionGitCommit,
			CommitMsg: fmt.Sprintf("commit %d (seed=%d)", e.stepCounter, e.seed),
			Timestamp: now,
		}
	case ActionGitBranch:
		bName := fmt.Sprintf("feature_%d", e.stepCounter)
		return Action{
			Type:      ActionGitBranch,
			Branch:    bName,
			Timestamp: now,
		}
	case ActionGitCheckout:
		bName := e.branches[e.rng.Intn(len(e.branches))]
		return Action{
			Type:      ActionGitCheckout,
			Branch:    bName,
			Timestamp: now,
		}
	case ActionGitTag:
		tName := fmt.Sprintf("v0.%d.0", e.stepCounter)
		return Action{
			Type:      ActionGitTag,
			Tag:       tName,
			Timestamp: now,
		}
	}

	return Action{Type: ActionGitAdd, Path: "."}
}

// ExecuteAndAssert 同时在 gogit 与 native git 镜像执行动作，并断言状态与树哈希绝对一致
func (e *DifferentialEngine) ExecuteAndAssert(act Action) {
	desc := e.describeAction(act)
	e.history = append(e.history, desc)

	switch act.Type {
	case ActionCreateFile:
		e.writeFileBoth(act.Path, act.Content)
		e.activeFiles[act.Path] = true
	case ActionModifyFile:
		e.writeFileBoth(act.Path, act.Content)
	case ActionDeleteFile:
		e.removeFileBoth(act.Path)
		delete(e.activeFiles, act.Path)
	case ActionRenameFile:
		e.renameFileBoth(act.OldPath, act.Path)
		delete(e.activeFiles, act.OldPath)
		e.activeFiles[act.Path] = true
	case ActionGitAdd:
		e.runBothCLI("add", act.Path)
	case ActionGitCommit:
		// 同步时间戳与提交者信息，确保 Commit SHA 具有可比性
		timeStr := fmt.Sprintf("%d +0000", act.Timestamp)
		env := []string{
			"GIT_AUTHOR_NAME=DiffBot",
			"GIT_AUTHOR_EMAIL=diffbot@google.com",
			"GIT_AUTHOR_DATE=" + timeStr,
			"GIT_COMMITTER_NAME=DiffBot",
			"GIT_COMMITTER_EMAIL=diffbot@google.com",
			"GIT_COMMITTER_DATE=" + timeStr,
		}
		// 首先使用 add . 暂存所有变更
		e.runBothCLIWithEnv(env, "add", ".")
		e.runBothCLIWithEnv(env, "commit", "-m", act.CommitMsg)
	case ActionGitBranch:
		e.runBothCLI("branch", act.Branch)
		e.branches = append(e.branches, act.Branch)
	case ActionGitCheckout:
		e.runBothCLI("checkout", act.Branch)
	case ActionGitMerge:
		timeStr := fmt.Sprintf("%d +0000", act.Timestamp)
		env := []string{
			"GIT_AUTHOR_NAME=DiffBot",
			"GIT_AUTHOR_EMAIL=diffbot@google.com",
			"GIT_AUTHOR_DATE=" + timeStr,
			"GIT_COMMITTER_NAME=DiffBot",
			"GIT_COMMITTER_EMAIL=diffbot@google.com",
			"GIT_COMMITTER_DATE=" + timeStr,
		}
		e.runBothCLIWithEnv(env, "merge", act.Branch)
	case ActionGitTag:
		e.runBothCLI("tag", act.Tag)
		e.tags = append(e.tags, act.Tag)
	}

	// === 核心预言机断言 ===
	// 1. 验证工作区状态 100% 对齐（git status --porcelain）
	statusGogit := e.runGogit("status", "--porcelain")
	statusNative := e.runNativeGit("status", "--porcelain")
	if statusGogit.stdout != statusNative.stdout {
		e.failDiscrepancy("Status Porcelain 不一致", statusGogit.stdout, statusNative.stdout)
	}

	// 2. 验证索引树对象哈希 100% 对齐（write-tree）
	treeGogit := e.runGogit("write-tree")
	treeNative := e.runNativeGit("write-tree")
	if treeGogit.exitCode == 0 && treeNative.exitCode == 0 {
		if strings.TrimSpace(treeGogit.stdout) != strings.TrimSpace(treeNative.stdout) {
			e.failDiscrepancy("Write-Tree 生成的 Tree SHA-1 不一致", treeGogit.stdout, treeNative.stdout)
		}
	}
}

// 辅助方法：生成随机文件路径（支持深层嵌套与特殊名字）
func (e *DifferentialEngine) randomFilePath() string {
	dirs := []string{"", "src", "pkg/util", "internal/engine/core", "nested/a/b/c"}
	names := []string{"main.go", "config.json", "data.bin", "helper.go", "service.proto", "README.md"}

	dir := dirs[e.rng.Intn(len(dirs))]
	name := names[e.rng.Intn(len(names))]

	if dir == "" {
		return name
	}
	return filepath.Join(dir, name)
}

// 辅助方法：生成随机文件内容（包含纯文本、空文件、二进制数据）
func (e *DifferentialEngine) randomContent() []byte {
	mode := e.rng.Intn(4)
	switch mode {
	case 0:
		// 空文件测试
		return []byte{}
	case 1:
		// 二进制空字节与特殊控制符测试
		b := make([]byte, 32)
		e.rng.Read(b)
		return b
	default:
		// 普通多行文本
		lines := []string{
			fmt.Sprintf("// generated at step %d", e.stepCounter),
			"package main",
			fmt.Sprintf("const Seed = %d", e.seed),
			"func init() { println(\"ready\") }",
		}
		return []byte(strings.Join(lines, "\n") + "\n")
	}
}

func (e *DifferentialEngine) pickActiveFile() string {
	files := make([]string, 0, len(e.activeFiles))
	for f := range e.activeFiles {
		files = append(files, f)
	}
	sort.Strings(files)
	return files[e.rng.Intn(len(files))]
}

func (e *DifferentialEngine) writeFileBoth(relPath string, content []byte) {
	p1 := filepath.Join(e.dirGogit, relPath)
	p2 := filepath.Join(e.dirNativeGit, relPath)
	_ = os.MkdirAll(filepath.Dir(p1), 0755)
	_ = os.MkdirAll(filepath.Dir(p2), 0755)
	if err := os.WriteFile(p1, content, 0644); err != nil {
		e.t.Fatalf("写 gogit 文件失败: %v", err)
	}
	if err := os.WriteFile(p2, content, 0644); err != nil {
		e.t.Fatalf("写 native git 文件失败: %v", err)
	}
}

func (e *DifferentialEngine) removeFileBoth(relPath string) {
	_ = os.Remove(filepath.Join(e.dirGogit, relPath))
	_ = os.Remove(filepath.Join(e.dirNativeGit, relPath))
}

func (e *DifferentialEngine) renameFileBoth(oldRel, newRel string) {
	p1Old := filepath.Join(e.dirGogit, oldRel)
	p1New := filepath.Join(e.dirGogit, newRel)
	p2Old := filepath.Join(e.dirNativeGit, oldRel)
	p2New := filepath.Join(e.dirNativeGit, newRel)

	_ = os.MkdirAll(filepath.Dir(p1New), 0755)
	_ = os.MkdirAll(filepath.Dir(p2New), 0755)

	_ = os.Rename(p1Old, p1New)
	_ = os.Rename(p2Old, p2New)
}

type cliResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func (e *DifferentialEngine) runGogit(args ...string) cliResult {
	cmdArgs := append([]string{"-C", e.dirGogit}, args...)
	cmd := exec.Command(e.gogitBin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return cliResult{exitCode: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (e *DifferentialEngine) runNativeGit(args ...string) cliResult {
	cmdArgs := append([]string{"-C", e.dirNativeGit, "-c", "core.excludesfile="}, args...)
	cmd := exec.Command("git", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return cliResult{exitCode: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (e *DifferentialEngine) runBothCLI(args ...string) {
	e.runBothCLIWithEnv(nil, args...)
}

func (e *DifferentialEngine) runBothCLIWithEnv(env []string, args ...string) {
	// Gogit 执行
	cmdGogit := exec.Command(e.gogitBin, append([]string{"-C", e.dirGogit}, args...)...)
	if len(env) > 0 {
		cmdGogit.Env = append(os.Environ(), env...)
	}
	var out1, err1 bytes.Buffer
	cmdGogit.Stdout = &out1
	cmdGogit.Stderr = &err1
	_ = cmdGogit.Run()

	// Native Git 执行
	cmdNative := exec.Command("git", append([]string{"-C", e.dirNativeGit, "-c", "core.excludesfile="}, args...)...)
	if len(env) > 0 {
		cmdNative.Env = append(os.Environ(), env...)
	}
	var out2, err2 bytes.Buffer
	cmdNative.Stdout = &out2
	cmdNative.Stderr = &err2
	_ = cmdNative.Run()
}

func (e *DifferentialEngine) describeAction(act Action) string {
	switch act.Type {
	case ActionCreateFile:
		return fmt.Sprintf("CreateFile(%s, %d bytes)", act.Path, len(act.Content))
	case ActionModifyFile:
		return fmt.Sprintf("ModifyFile(%s, %d bytes)", act.Path, len(act.Content))
	case ActionDeleteFile:
		return fmt.Sprintf("DeleteFile(%s)", act.Path)
	case ActionRenameFile:
		return fmt.Sprintf("RenameFile(%s -> %s)", act.OldPath, act.Path)
	case ActionGitAdd:
		return fmt.Sprintf("GitAdd(%s)", act.Path)
	case ActionGitCommit:
		return fmt.Sprintf("GitCommit(%q)", act.CommitMsg)
	case ActionGitBranch:
		return fmt.Sprintf("GitBranch(%s)", act.Branch)
	case ActionGitCheckout:
		return fmt.Sprintf("GitCheckout(%s)", act.Branch)
	case ActionGitTag:
		return fmt.Sprintf("GitTag(%s)", act.Tag)
	default:
		return fmt.Sprintf("%v", act.Type)
	}
}

func (e *DifferentialEngine) failDiscrepancy(reason, gogitVal, nativeVal string) {
	e.t.Fatalf(`
==============================
差分测试发现不一致 (DIFFERENTIAL BUG DETECTED)
原因: %s
Seed: %d (复现请运行: SEED=%d go test -v ./test/differential/...)
历史操作序列:
  %s

--- gogit 输出 ---
%s
--- 原生 git 输出 ---
%s
==============================
`, reason, e.seed, e.seed, strings.Join(e.history, "\n  "), gogitVal, nativeVal)
}

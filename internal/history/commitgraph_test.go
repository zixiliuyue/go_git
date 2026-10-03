package history

import (
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCommitGraphFormatAndNativeVerify(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	user := object.Signature{
		Name:  "Graph Tester",
		Email: "tester@google.com",
		When:  time.Unix(1700000000, 0),
		TZ:    "+0000",
	}

	bHash, err := r.WriteBlob([]byte("file 1"))
	if err != nil {
		t.Fatalf("WriteBlob failed: %v", err)
	}

	tree1 := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "file1.txt", OID: bHash},
		},
	}
	t1Hash, err := r.WriteTree(tree1)
	if err != nil {
		t.Fatalf("WriteTree failed: %v", err)
	}

	c1 := &object.Commit{
		Tree:      t1Hash,
		Author:    user,
		Committer: user,
		Message:   "commit 1\n",
	}
	c1Hash, err := r.WriteCommit(c1)
	if err != nil {
		t.Fatalf("WriteCommit c1 failed: %v", err)
	}

	bHash2, _ := r.WriteBlob([]byte("file 2"))
	tree2 := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "file2.txt", OID: bHash2},
		},
	}
	t2Hash, _ := r.WriteTree(tree2)
	c2 := &object.Commit{
		Tree:      t2Hash,
		Parents:   []object.Hash{c1Hash},
		Author:    user,
		Committer: user,
		Message:   "commit 2\n",
	}
	c2Hash, _ := r.WriteCommit(c2)

	c3 := &object.Commit{
		Tree:      t2Hash,
		Parents:   []object.Hash{c1Hash},
		Author:    user,
		Committer: user,
		Message:   "branch commit\n",
	}
	c3Hash, _ := r.WriteCommit(c3)

	c4 := &object.Commit{
		Tree:      t2Hash,
		Parents:   []object.Hash{c2Hash, c3Hash},
		Author:    user,
		Committer: user,
		Message:   "merge commit\n",
	}
	c4Hash, _ := r.WriteCommit(c4)

	_ = r.Refs.UpdateRef("refs/heads/main", c4Hash, user, "commit 4")

	cgPath, err := WriteCommitGraph(r, true)
	if err != nil {
		t.Fatalf("WriteCommitGraph failed: %v", err)
	}

	if _, err := os.Stat(cgPath); err != nil {
		t.Fatalf("commit-graph file does not exist: %v", err)
	}

	// 1. gogit 自我读取并验证
	cg, err := pack.ReadCommitGraph(cgPath)
	if err != nil {
		t.Fatalf("ReadCommitGraph failed: %v", err)
	}
	if cg.NumCommits != 4 {
		t.Fatalf("expected 4 commits, got %d", cg.NumCommits)
	}

	e4, ok := cg.Get(c4Hash)
	if !ok {
		t.Fatalf("commit c4 not found in commit-graph")
	}
	if len(e4.Parents) != 2 || e4.Parents[0] != c2Hash || e4.Parents[1] != c3Hash {
		t.Fatalf("c4 parents mismatch: %+v", e4.Parents)
	}
	if e4.Generation != 3 {
		t.Fatalf("expected generation 3 for merge commit, got %d", e4.Generation)
	}

	// 2. 自检验证
	if err := VerifyCommitGraph(cgPath, r); err != nil {
		t.Fatalf("VerifyCommitGraph failed: %v", err)
	}

	// 3. 原生 Git 对比校验 (git commit-graph verify)
	gitCmd := exec.Command("git", "commit-graph", "verify")
	gitCmd.Dir = tmpDir
	gitCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	output, err := gitCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native 'git commit-graph verify' failed: %v\nOutput: %s", err, string(output))
	}
}

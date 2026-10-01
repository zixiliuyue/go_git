package cli

import (
	"bytes"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestM4NativeSmartHTTPCloneAndPush(t *testing.T) {
	// 查找系统 git 与 git-http-backend
	gitPath, err := exec.LookPath("git")
	if err != nil || gitPath == "" {
		t.Skip("系统未安装原生 git，跳过原生 HTTP 后端集成测试")
	}

	execPathOut, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("无法获取 git --exec-path")
	}
	gitHttpBackend := filepath.Join(strings.TrimSpace(string(execPathOut)), "git-http-backend")
	if _, err := os.Stat(gitHttpBackend); err != nil {
		t.Skipf("git-http-backend 不存在: %s", gitHttpBackend)
	}

	tmpDir, err := os.MkdirTemp("", "gogit-m4-http-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 解析真实路径以防软链接
	realTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err == nil {
		tmpDir = realTmpDir
	}

	// 1. 初始化中央裸仓库 central.git
	centralPath := filepath.Join(tmpDir, "central.git")
	cmd := exec.Command("git", "init", "--bare", centralPath, "-b", "main")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare 失败: %v, %s", err, string(out))
	}

	// 开启 http.receivepack
	cmd = exec.Command("git", "-C", centralPath, "config", "http.receivepack", "true")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	_ = cmd.Run()

	// 2. 原生 git 提交初始内容到 central.git
	initWork := filepath.Join(tmpDir, "init_work")
	cmd = exec.Command("git", "init", initWork, "-b", "main")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	_ = cmd.Run()

	_ = os.WriteFile(filepath.Join(initWork, "readme.txt"), []byte("hello http world\n"), 0644)
	cmd = exec.Command("git", "-C", initWork, "add", "readme.txt")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	_ = cmd.Run()

	cmd = exec.Command("git", "-C", initWork, "-c", "user.name=Tester", "-c", "user.email=tester@example.com", "commit", "-m", "Initial http commit")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	_ = cmd.Run()

	cmd = exec.Command("git", "-C", initWork, "push", centralPath, "main")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("推送初始提交到 central.git 失败: %v, %s", err, string(out))
	}

	// 3. 启动基于原生 git-http-backend 的 httptest.Server
	cgiHandler := &cgi.Handler{
		Path: gitHttpBackend,
		Dir:  tmpDir,
		Env: []string{
			"GIT_PROJECT_ROOT=" + tmpDir,
			"GIT_HTTP_EXPORT_ALL=1",
			"PATH=" + os.Getenv("PATH"),
		},
	}
	ts := httptest.NewServer(cgiHandler)
	defer ts.Close()

	// 4. gogit clone 通过 HTTP 协议克隆 central.git
	cloneDir := filepath.Join(tmpDir, "http_clone")
	var stdout, stderr bytes.Buffer
	cloneURL := ts.URL + "/central.git"

	code := cmdClone(&Context{
		Args:   []string{cloneURL, cloneDir},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("gogit clone over HTTP 失败: code=%d, stderr=%s", code, stderr.String())
	}

	readmeFile := filepath.Join(cloneDir, "readme.txt")
	content, err := os.ReadFile(readmeFile)
	if err != nil || string(content) != "hello http world\n" {
		t.Fatalf("HTTP 克隆文件内容校验失败: %s", string(content))
	}

	// 5. 跨系统校验克隆仓库的完整性
	cmd = exec.Command("git", "-C", cloneDir, "fsck", "--strict")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("克隆仓库 git fsck 失败: %v, %s", err, string(out))
	}

	// 6. 在克隆仓库创建本地提交，通过 HTTP 推送回 central.git
	origDir, _ := os.Getwd()
	_ = os.Chdir(cloneDir)
	defer os.Chdir(origDir)

	_ = os.WriteFile(filepath.Join(cloneDir, "client_patch.txt"), []byte("patch via http push\n"), 0644)
	stdout.Reset()
	stderr.Reset()
	_ = cmdAdd(&Context{Args: []string{"client_patch.txt"}, Stdout: &stdout, Stderr: &stderr})
	_ = cmdCommit(&Context{Args: []string{"-m", "Pushed over HTTP protocol"}, Stdout: &stdout, Stderr: &stderr})

	stdout.Reset()
	stderr.Reset()
	code = cmdPush(&Context{Args: []string{"origin", "main"}, Stdout: &stdout, Stderr: &stderr})
	if code != ExitSuccess {
		t.Fatalf("gogit push over HTTP 失败: code=%d, stderr=%s", code, stderr.String())
	}

	// 7. 验证中央仓库收到该提交，且 git fsck --strict 零错误
	cmd = exec.Command("git", "-C", centralPath, "fsck", "--strict")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("推送后 central.git git fsck 失败: %v, %s", err, string(out))
	}

	cmd = exec.Command("git", "-C", centralPath, "log", "-1", "--pretty=format:%s")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	logOut, _ := cmd.CombinedOutput()
	if string(logOut) != "Pushed over HTTP protocol" {
		t.Fatalf("中央仓库最新提交不匹配: %s", string(logOut))
	}
}

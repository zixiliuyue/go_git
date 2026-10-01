package hook

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	ErrHookFailed = errors.New("hook execution failed")
)

// RunHook 查找并执行指定 Git 钩子脚本（若不存在或不可执行则静默跳过）
func RunHook(gitDir, hookName string, args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	hookPath := filepath.Join(gitDir, "hooks", hookName)
	fi, err := os.Stat(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// 检查是否具备可执行权限
	if fi.Mode()&0111 == 0 {
		return nil
	}

	cmd := exec.Command(hookPath, args...)
	cmd.Dir = filepath.Dir(gitDir) // 通常在工作区根目录执行
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("%w: hook %s exited with status %d", ErrHookFailed, hookName, exitErr.ExitCode())
		}
		return fmt.Errorf("%w: %s: %v", ErrHookFailed, hookName, err)
	}

	return nil
}

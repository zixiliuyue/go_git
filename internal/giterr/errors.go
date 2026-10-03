package giterr

import (
	"context"
	"errors"
	"fmt"
	"gogit/internal/object"
)

// Sentinel Errors: 标准领域哨兵错误，支持 errors.Is 匹配
var (
	// ErrObjectNotFound 对象在仓库中未找到
	ErrObjectNotFound = errors.New("object not found")

	// ErrCorruptedIndex 索引文件格式损坏或校验和不匹配
	ErrCorruptedIndex = errors.New("corrupted index file")

	// ErrMergeConflict 合并过程中检测到不可自动解决的代码冲突
	ErrMergeConflict = errors.New("merge conflict detected")

	// ErrInvalidRefFormat 引用名称不符合 Git 规范格式
	ErrInvalidRefFormat = errors.New("invalid ref format")

	// ErrNotAGitRepository 路径不在任何有效的 Git 仓库中
	ErrNotAGitRepository = errors.New("not a git repository (or any of the parent directories)")

	// ErrLockAcquisitionFailed 获取文件锁失败（已有并发操作在进行）
	ErrLockAcquisitionFailed = errors.New("unable to acquire lock")

	// ErrRefNotFound 目标引用不存在
	ErrRefNotFound = errors.New("reference not found")

	// ErrInterrupted 操作被用户信号中断 (SIGINT/Ctrl+C 或 SIGTERM)
	ErrInterrupted = errors.New("operation canceled or interrupted")

	// ErrBareRepository 该操作要求在具有工作区的非裸仓库中执行
	ErrBareRepository = errors.New("this operation must be run in a work tree")
)

// ObjectNotFoundError 携带具体缺失 OID 的结构化类型错误
type ObjectNotFoundError struct {
	OID object.Hash
}

func (e *ObjectNotFoundError) Error() string {
	return fmt.Sprintf("对象未找到: %s", e.OID.String())
}

func (e *ObjectNotFoundError) Is(target error) bool {
	return target == ErrObjectNotFound
}

// MergeConflictError 携带冲突文件及原因的结构化类型错误
type MergeConflictError struct {
	Path   string
	Reason string
}

func (e *MergeConflictError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("合并冲突 [%s]: %s", e.Path, e.Reason)
	}
	return fmt.Sprintf("合并冲突: %s", e.Path)
}

func (e *MergeConflictError) Is(target error) bool {
	return target == ErrMergeConflict
}

// CorruptedIndexError 携带索引路径及损坏详情的结构化类型错误
type CorruptedIndexError struct {
	Path   string
	Reason string
}

func (e *CorruptedIndexError) Error() string {
	return fmt.Sprintf("索引文件损坏 (%s): %s", e.Path, e.Reason)
}

func (e *CorruptedIndexError) Is(target error) bool {
	return target == ErrCorruptedIndex
}

// LockError 携带被锁定路径与底层原因的结构化类型错误
type LockError struct {
	Path string
	Err  error
}

func (e *LockError) Error() string {
	return fmt.Sprintf("锁定失败 (%s): %v", e.Path, e.Err)
}

func (e *LockError) Is(target error) bool {
	return target == ErrLockAcquisitionFailed
}

func (e *LockError) Unwrap() error {
	return e.Err
}

// ExitCoder 允许自定义错误显式指定 Git 退出码
type ExitCoder interface {
	ExitCode() int
}

// ExitCodeForError 根据错误类型精确计算并映射 Git 标准退出码:
//   - 0: 成功 (nil)
//   - 1: 业务判断/冲突 (如 MergeConflict)
//   - 128: 致命错误 (如对象缺失、格式损坏、非 Git 仓库)
//   - 130: 信号中断 (SIGINT / 128 + 2)
func ExitCodeForError(err error) int {
	if err == nil {
		return 0
	}

	// 1. 若错误实现了 ExitCoder 接口，直接使用其定义的退出码
	var ec ExitCoder
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}

	// 2. 检查是否为信号中断或 context 取消 (SIGINT 标准退出码 130)
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrInterrupted) {
		return 130
	}

	// 3. 合并冲突或业务不满足属于一般退出状态码 1
	if errors.Is(err, ErrMergeConflict) {
		return 1
	}

	// 4. 其余领域错误与未知致命异常均返回 Git 规范的 ExitFatal 128
	return 128
}

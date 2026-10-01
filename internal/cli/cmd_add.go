package cli

import (
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("add", cmdAdd)
}

func cmdAdd(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit add <pathspec>...")
		return ExitGeneral
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if r.IsBare {
		fmt.Fprintln(ctx.Stderr, "fatal: 不能在 bare 仓库中执行 add")
		return ExitFatal
	}

	idx, err := r.GetIndex()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取索引失败: %v\n", err)
		return ExitFatal
	}

	for _, spec := range ctx.Args {
		if spec == "-A" || spec == "." {
			spec = "."
		}

		targetPath := filepath.Join(r.WorkTree, spec)
		fi, err := os.Stat(targetPath)
		if err != nil {
			// 如果工作区文件已被删除，从 index 中移除该条目
			rel, relErr := filepath.Rel(r.WorkTree, targetPath)
			if relErr == nil {
				idx.RemoveEntry(rel)
				continue
			}
			fmt.Fprintf(ctx.Stderr, "fatal: 路径未找到: %s\n", spec)
			return ExitFatal
		}

		if fi.IsDir() {
			err = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				// 跳过 .git 目录
				if info.IsDir() && info.Name() == ".git" {
					return filepath.SkipDir
				}
				if info.IsDir() {
					return nil
				}

				return stageFile(r, idx, path, info)
			})
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 遍历添加文件失败: %v\n", err)
				return ExitFatal
			}
		} else {
			if err := stageFile(r, idx, targetPath, fi); err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 添加文件 %s 失败: %v\n", spec, err)
				return ExitFatal
			}
		}
	}

	if err := r.SaveIndex(idx); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 保存索引失败: %v\n", err)
		return ExitFatal
	}

	return ExitSuccess
}

func stageFile(r *repo.Repository, idx *index.Index, absPath string, fi os.FileInfo) error {
	relPath, err := filepath.Rel(r.WorkTree, absPath)
	if err != nil {
		return err
	}
	// 规范化路径分隔符为正斜杠 '/'
	relPath = strings.ReplaceAll(relPath, "\\", "/")

	var data []byte
	if fi.Mode()&os.ModeSymlink != 0 {
		// 符号链接内容为目标链接地址
		target, err := os.Readlink(absPath)
		if err != nil {
			return err
		}
		data = []byte(target)
	} else {
		data, err = os.ReadFile(absPath)
		if err != nil {
			return err
		}
	}

	blobHash, err := object.WriteLooseObjectToDir(r.ObjectsDir, object.TypeBlob, data)
	if err != nil {
		return err
	}

	entry := index.EntryFromOSFileInfo(relPath, fi, blobHash)
	idx.AddOrReplaceEntry(entry)
	return nil
}

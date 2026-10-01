package cli

import (
	"fmt"
	"gogit/internal/index"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
)

func init() {
	Register("add", cmdAdd)
}

func cmdAdd(ctx *Context) int {
	force := false
	var specs []string
	for _, arg := range ctx.Args {
		if arg == "-f" || arg == "--force" {
			force = true
		} else {
			specs = append(specs, arg)
		}
	}

	if len(specs) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit add [-f] <pathspec>...")
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

	ignorer := worktree.NewIgnoreMatcher()
	_ = ignorer.LoadIgnoreFile(filepath.Join(r.WorkTree, ".gitignore"), "")
	_ = ignorer.LoadIgnoreFile(filepath.Join(r.GitDir, "info", "exclude"), "")
	if globalExclude := r.Config.Get("core", "excludesfile"); globalExclude != "" {
		_ = ignorer.LoadIgnoreFile(globalExclude, "")
	} else if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		_ = ignorer.LoadIgnoreFile(filepath.Join(xdg, "git", "ignore"), "")
	} else if home, err := os.UserHomeDir(); err == nil {
		_ = ignorer.LoadIgnoreFile(filepath.Join(home, ".config", "git", "ignore"), "")
	}

	for _, spec := range specs {
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

				relPath, _ := filepath.Rel(r.WorkTree, path)
				relPath = filepath.ToSlash(relPath)

				if info.IsDir() {
					localIgnore := filepath.Join(path, ".gitignore")
					if _, err := os.Stat(localIgnore); err == nil {
						_ = ignorer.LoadIgnoreFile(localIgnore, relPath)
					}
					if !force && ignorer.Match(relPath, true) {
						return filepath.SkipDir
					}
					return nil
				}

				if !force && ignorer.Match(relPath, false) {
					return nil
				}

				return stageFile(r, idx, path, info)
			})
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 遍历添加文件失败: %v\n", err)
				return ExitFatal
			}
		} else {
			relPath, _ := filepath.Rel(r.WorkTree, targetPath)
			relPath = filepath.ToSlash(relPath)
			if !force && ignorer.Match(relPath, false) {
				fmt.Fprintf(ctx.Stderr, "The following paths are ignored by one of your .gitignore files:\n%s\nUse -f if you really want to add them.\n", relPath)
				return ExitGeneral
			}
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
	relPath = filepath.ToSlash(relPath)

	var data []byte
	if fi.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(absPath)
		if err != nil {
			return err
		}
		data = []byte(linkTarget)
	} else {
		data, err = os.ReadFile(absPath)
		if err != nil {
			return err
		}
	}

	oid, err := r.WriteBlob(data)
	if err != nil {
		return fmt.Errorf("写入 blob 对象失败: %w", err)
	}

	entry := index.EntryFromOSFileInfo(relPath, fi, oid)
	idx.AddOrReplaceEntry(entry)
	return nil
}

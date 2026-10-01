package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
)

func init() {
	Register("pack-refs", cmdPackRefs)
}

func cmdPackRefs(ctx *Context) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	r, err := repo.FindRepository(cwd)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
		return ExitFatal
	}

	all := false
	prune := true

	for _, arg := range ctx.Args {
		if arg == "--all" {
			all = true
		} else if arg == "--no-prune" {
			prune = false
		} else if arg == "--prune" {
			prune = true
		}
	}

	peeler := func(h object.Hash) (object.Hash, bool) {
		raw, err := r.ReadObject(h)
		if err == nil && raw.ObjType == object.TypeTag {
			tag, err := object.ParseTag(raw.Payload())
			if err == nil {
				return tag.Object, true
			}
		}
		return object.ZeroHash, false
	}

	if err := r.Refs.PackRefsWithPeeler(all, prune, peeler); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: pack-refs 失败: %v\n", err)
		return ExitFatal
	}

	return ExitSuccess
}

package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"strconv"
	"strings"
)

func init() {
	Register("rev-parse", cmdRevParse)
}

func cmdRevParse(ctx *Context) int {
	shortLen := 0
	verify := false
	var revs []string

	for _, arg := range ctx.Args {
		if arg == "--verify" {
			verify = true
		} else if arg == "--short" {
			shortLen = 7
		} else if strings.HasPrefix(arg, "--short=") {
			l, err := strconv.Atoi(strings.TrimPrefix(arg, "--short="))
			if err == nil && l >= 4 && l <= 40 {
				shortLen = l
			} else {
				shortLen = 7
			}
		} else if !strings.HasPrefix(arg, "-") {
			revs = append(revs, arg)
		}
	}

	if len(revs) == 0 {
		return ExitGeneral
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	hasError := false
	for _, expr := range revs {
		h, err := rev.ParseRevision(r, expr)
		if err != nil {
			if verify {
				fmt.Fprintf(ctx.Stderr, "fatal: Needed a single revision\n")
				return ExitFatal
			}
			hasError = true
			continue
		}

		res := h.String()
		if shortLen > 0 && shortLen < len(res) {
			res = res[:shortLen]
		}
		fmt.Fprintln(ctx.Stdout, res)
	}

	if hasError {
		return ExitFatal
	}
	return ExitSuccess
}

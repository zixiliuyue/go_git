package main

import (
	"gogit/internal/cli"
	"os"
)

func main() {
	exitCode := cli.Run(os.Args[1:])
	os.Exit(exitCode)
}

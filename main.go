package main

import (
	"os"

	cmd "github.com/yifanes/miniclawd/cmd/miniclawd"
)

func main() {
	os.Exit(cmd.Run())
}

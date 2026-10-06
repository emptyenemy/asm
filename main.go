package main

import (
	"os"

	"github.com/emptyenemy/asm/internal/cli"
)

var version = "1.0.0"

func main() {
	if cli.Run(os.Args[1:], version) != nil {
		os.Exit(1)
	}
}

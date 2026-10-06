package main

import (
	"fmt"
	"os"

	"github.com/emptyenemy/asm/internal/cli"
)

var version = "1.0.0"

func main() {
	if err := cli.Run(os.Args[1:], version); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

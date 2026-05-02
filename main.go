package main

import (
	"fmt"
	"os"

	"github.com/JinBa1/my-time-has-come/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

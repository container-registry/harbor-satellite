package main

import (
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/cmd"
)

func main() {
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

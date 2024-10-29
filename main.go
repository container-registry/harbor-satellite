package main

import (
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/cmd"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

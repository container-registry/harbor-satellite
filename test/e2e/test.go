package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	// Command to execute
	cmd := exec.Command("go", "run", "./main.go")

	// Get stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("Error creating stdout pipe:", err)
		os.Exit(1)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		fmt.Println("Error starting command:", err)
		os.Exit(1)
	}

	// Create a scanner to read the command output line by line
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line) // Print each line of output

		lineParts := strings.Split(line, "--")
		// Check if the line contains "----"
		if len(lineParts) > 2 {
			fmt.Println("Satellite is Working...\nExiting...")
			cmd.Process.Kill() // Kill the process
			os.Exit(0)         // Exit the program
		}
	}

	// Handle any scanner error
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading stdout:", err)
		os.Exit(1)
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		fmt.Println("Command execution failed:", err)
		os.Exit(1)
	}
}

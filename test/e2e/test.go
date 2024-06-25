package main

import (
	"bufio"
	"fmt"
	"log"
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
		log.Fatalf("Error creating stdout pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		log.Fatalf("Error starting command: %v", err)
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
			if err := cmd.Process.Kill(); err != nil {
				fmt.Println("Error killing process:", err)
			}
			os.Exit(0) // Exit the program
		}
	}

	// Handle any scanner error
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading stdout: %v", err)
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		log.Fatalf("Command execution failed: %v", err)
	}
}

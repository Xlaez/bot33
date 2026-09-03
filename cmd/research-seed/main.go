package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	script := "scripts/research_seed_fast.py"
	if len(os.Args) > 1 {
		script = os.Args[1]
	}
	cmd := exec.Command("python3", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = mustWD()
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "research-seed failed: %v\n", err)
		os.Exit(1)
	}
}

func mustWD() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

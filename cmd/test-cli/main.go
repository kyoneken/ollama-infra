package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Test CLI startup in Docker environment
func main() {
	fmt.Fprintf(os.Stderr, "[test-cli] Attempting to execute copilot binary...\n")
	
	// Try direct execution
	cmd := exec.Command("/usr/local/bin/copilot", "--version")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// Capture actual error
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[test-cli] Direct execution failed: %v\n", err)
		
		// Check if it's a permission issue
		info, err := os.Stat("/usr/local/bin/copilot")
		if err == nil {
			fmt.Fprintf(os.Stderr, "[test-cli] Binary exists: size=%d, mode=%o\n", info.Size(), info.Mode())
		}
		
		// Try ldd (may not be available)
		fmt.Fprintf(os.Stderr, "[test-cli] Checking dynamic dependencies...\n")
		lddCmd := exec.Command("ldd", "/usr/local/bin/copilot")
		out, err := lddCmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[test-cli] ldd failed: %v (output: %s)\n", err, string(out))
		} else {
			fmt.Fprintf(os.Stderr, "[test-cli] ldd output: %s\n", string(out))
		}
		
		// Check system info
		fmt.Fprintf(os.Stderr, "[test-cli] System: %v\n", syscall.GOARCH)
		os.Exit(1)
	}
	
	fmt.Fprintf(os.Stderr, "[test-cli] CLI executed successfully!\n")
}

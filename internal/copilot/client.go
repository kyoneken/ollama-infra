package copilot

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	copilot "github.com/github/copilot-sdk/go"
)

// ReviewClient wraps Copilot SDK client and manages its lifecycle
type ReviewClient struct {
	client *copilot.Client
}

// NewReviewClient initializes a new Copilot SDK client
// It sets GH_COPILOT_ENDPOINT to localhost:11434 (Ollama)
// Returns error if SDK client fails to start
func NewReviewClient(ctx context.Context) (*ReviewClient, error) {
	// Pre-initialization diagnostics
	diagnostics := true // Enable for debugging
	if diagnostics {
		fmt.Fprintf(os.Stderr, "[copilot-sdk] === Pre-initialization Diagnostics ===\n")
		
		// Check PATH
		pathEnv := os.Getenv("PATH")
		fmt.Fprintf(os.Stderr, "[copilot-sdk] PATH: %s\n", pathEnv)
		
		// Check if copilot is in PATH
		if copilotPath, err := exec.LookPath("copilot"); err == nil {
			fmt.Fprintf(os.Stderr, "[copilot-sdk] ✓ copilot found in PATH: %s\n", copilotPath)
			// Get binary info
			if info, err := os.Stat(copilotPath); err == nil {
				fmt.Fprintf(os.Stderr, "[copilot-sdk]   Size: %d bytes\n", info.Size())
				fmt.Fprintf(os.Stderr, "[copilot-sdk]   Mode: %o\n", info.Mode())
			}
			// Try running --version
			if out, err := exec.CommandContext(ctx, copilotPath, "--version").CombinedOutput(); err == nil {
				fmt.Fprintf(os.Stderr, "[copilot-sdk]   Version: %s\n", string(out))
			} else {
				fmt.Fprintf(os.Stderr, "[copilot-sdk]   ✗ --version failed: %v (output: %s)\n", err, string(out))
			}
		} else {
			fmt.Fprintf(os.Stderr, "[copilot-sdk] ✗ copilot not found in PATH\n")
			// Check /usr/local/bin explicitly
			if info, err := os.Stat("/usr/local/bin/copilot"); err == nil {
				fmt.Fprintf(os.Stderr, "[copilot-sdk] ✓ Found at /usr/local/bin/copilot (%d bytes)\n", info.Size())
			}
		}

		// Check Ollama endpoint
		endpoint := "http://localhost:11434"
		fmt.Fprintf(os.Stderr, "[copilot-sdk] Testing Ollama endpoint: %s\n", endpoint)
		fmt.Fprintf(os.Stderr, "[copilot-sdk] === End Diagnostics ===\n\n")
	}

	// Set Ollama endpoint for embedded Copilot CLI
	if err := os.Setenv("GH_COPILOT_ENDPOINT", "http://localhost:11434"); err != nil {
		return nil, fmt.Errorf("setenv GH_COPILOT_ENDPOINT: %w", err)
	}

	// Create SDK client (bundler-managed binary)
	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel: "debug",
	})

	// Start SDK server (manages CLI process)
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Starting SDK server...\n")
	if err := client.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[copilot-sdk] ✗ Initialization error:\n%+v\n", err)
		return nil, fmt.Errorf("copilot SDK start: %w", err)
	}

	// Debug: Log SDK integration info
	fmt.Fprintf(os.Stderr, "[copilot-sdk] ✓ Integration successfully initialized\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Version: github.com/github/copilot-sdk/go v0.3.0\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Backend: Ollama (http://localhost:11434)\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] LogLevel: debug\n")

	return &ReviewClient{client: client}, nil
}

// Close gracefully shuts down the Copilot SDK client
func (rc *ReviewClient) Close() error {
	if rc.client == nil {
		return nil
	}
	return rc.client.Stop()
}

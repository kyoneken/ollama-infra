package copilot

import (
	"context"
	"fmt"
	"os"

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
	// Set Ollama endpoint for embedded Copilot CLI
	if err := os.Setenv("GH_COPILOT_ENDPOINT", "http://localhost:11434"); err != nil {
		return nil, fmt.Errorf("setenv GH_COPILOT_ENDPOINT: %w", err)
	}

	// Create SDK client (bundler-managed binary)
	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel: "error",
	})

	// Start SDK server (manages CLI process)
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("copilot SDK start: %w", err)
	}

	// Debug: Log SDK integration info
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Integration successfully initialized\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Version: github.com/github/copilot-sdk/go v0.3.0\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] Backend: Ollama (http://localhost:11434)\n")
	fmt.Fprintf(os.Stderr, "[copilot-sdk] LogLevel: error\n")

	return &ReviewClient{client: client}, nil
}

// Close gracefully shuts down the Copilot SDK client
func (rc *ReviewClient) Close() error {
	if rc.client == nil {
		return nil
	}
	return rc.client.Stop()
}

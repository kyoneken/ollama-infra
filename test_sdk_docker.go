package main

import (
"context"
"fmt"
"os"
"time"

copilot "github.com/github/copilot-sdk/go"
)

// Standalone test to verify SDK works in Docker
func testSDKInDocker() {
fmt.Println("[Docker SDK Test] Starting...\n")

endpoint := os.Getenv("GH_COPILOT_ENDPOINT")
fmt.Printf("GH_COPILOT_ENDPOINT=%s\n", endpoint)

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

fmt.Print("\n[1/3] Starting SDK client... ")
client := copilot.NewClient(&copilot.ClientOptions{
LogLevel: "debug",
})

err := client.Start(ctx)
if err != nil {
fmt.Printf("FAILED: %v\n", err)
fmt.Fprintf(os.Stderr, "[docker-test] SDK Start error details: %T\n", err)
return
}
defer client.Stop()
fmt.Println("OK")

fmt.Print("[2/3] Creating session... ")
session, err := client.CreateSession(ctx, &copilot.SessionConfig{
Model: "",
OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
})
if err != nil {
fmt.Printf("FAILED: %v\n", err)
return
}
defer session.Disconnect()
fmt.Println("OK")

fmt.Print("[3/3] Sending test prompt... ")
_, err = session.Send(ctx, copilot.MessageOptions{
Prompt: "Test",
})
if err != nil {
fmt.Printf("FAILED: %v\n", err)
return
}
fmt.Println("OK")

fmt.Println("\n[Docker SDK Test] SUCCESS - SDK is functional in Docker!")
}

#!/bin/bash
set -e

# Configure Copilot CLI to use local Ollama endpoint
export GH_COPILOT_ENDPOINT="http://localhost:11434"
export GH_COPILOT_OFFLINE_MODE="true"

echo "[Copilot Config] Endpoint: $GH_COPILOT_ENDPOINT"
echo "[Copilot Config] Offline mode: $GH_COPILOT_OFFLINE_MODE"

# Wait for Ollama to be ready
echo "[Copilot Config] Waiting for Ollama to be ready..."
TIMEOUT=30
INTERVAL=1
ELAPSED=0

while [ $ELAPSED -lt $TIMEOUT ]; do
  if curl -sf http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "[Copilot Config] Ollama is ready at localhost:11434"
    break
  fi
  
  ELAPSED=$((ELAPSED + INTERVAL))
  if [ $ELAPSED -eq $TIMEOUT ]; then
    echo "[Copilot Config] Warning: Ollama not ready after $TIMEOUT seconds, but proceeding anyway"
    break
  fi
  
  sleep $INTERVAL
done

echo "[Copilot Config] Configuration complete"

#!/usr/bin/env bash
set -euo pipefail

REVIEW_OUTPUT="${REVIEW_OUTPUT:-/tmp/review.txt}"
DIFF_FILE="/tmp/pr.diff"
# Use COPILOT_MODEL env var if set; fall back to default
BASE_MODEL="${COPILOT_MODEL:-qwen2.5-coder:1.5b}"
REVIEW_MODEL="qwen-reviewer"

log() { echo "[entrypoint] $*"; }

# --- Start Ollama ---
log "Starting ollama serve..."
ollama serve &
OLLAMA_PID=$!

# --- Wait for Ollama to be ready ---
log "Waiting for Ollama to be ready (timeout 60s)..."
READY=false
for i in $(seq 1 60); do
  if curl -sf http://localhost:11434 > /dev/null 2>&1; then
    READY=true
    break
  fi
  sleep 1
done

if [[ "$READY" != "true" ]]; then
  log "ERROR: Ollama did not become ready within 60 seconds."
  exit 1
fi
log "Ollama is ready."

# --- Pull model ---
log "Pulling model: ${BASE_MODEL} ..."
ollama pull "${BASE_MODEL}"

# --- Create a context-limited model to speed up CPU inference ---
# Copilot CLI's built-in system prompt alone exceeds 10,000 tokens, making it
# incompatible with small context windows. We call the Ollama API directly to
# keep the total prompt small (~400 tokens) and inference fast (~30s on CPU).
log "Creating context-limited model '${REVIEW_MODEL}' (num_ctx 4096)..."
cat > /tmp/Modelfile <<EOF
FROM ${BASE_MODEL}
PARAMETER num_ctx 4096
EOF
ollama create "${REVIEW_MODEL}" -f /tmp/Modelfile

# --- Resolve diff input ---
if [[ -n "${PR_DIFF:-}" ]]; then
  log "Writing PR_DIFF env var to ${DIFF_FILE}..."
  printf '%s' "$PR_DIFF" > "$DIFF_FILE"
elif [[ -f /workspace/pr.diff ]]; then
  log "Using /workspace/pr.diff..."
  cp /workspace/pr.diff "$DIFF_FILE"
else
  log "ERROR: No diff found. Set PR_DIFF env var or provide /workspace/pr.diff."
  exit 1
fi

# --- Authenticate gh / copilot if token provided ---
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  log "GITHUB_TOKEN detected; configuring gh auth..."
  echo "$GITHUB_TOKEN" | gh auth login --with-token 2>/dev/null || true
fi

# --- Truncate diff ---
# Keep under ~2000 chars (~500 tokens) so total prompt stays well within 4096
MAX_DIFF_CHARS=2000
DIFF_CONTENT=$(head -c "${MAX_DIFF_CHARS}" "${DIFF_FILE}")
DIFF_LEN=$(wc -c < "${DIFF_FILE}")
if [[ "${DIFF_LEN}" -gt "${MAX_DIFF_CHARS}" ]]; then
  DIFF_CONTENT="${DIFF_CONTENT}
[... diff truncated at ${MAX_DIFF_CHARS} chars ...]"
  log "Diff truncated from ${DIFF_LEN} chars to ${MAX_DIFF_CHARS} chars."
fi

# --- Run code review via Ollama API directly ---
# Copilot CLI's built-in system prompt is ~10,000+ tokens which exceeds the
# model context window, making it unusable for CPU inference in CI.
# curl handles HTTP chunked streaming natively; jq --unbuffered extracts tokens
# and flushes each one immediately, eliminating all Python buffering issues.
log "Running streaming code review (120s timeout)..."

SYSTEM_PROMPT="Review this diff. For each bug, typo, or comment mismatch output:
FILE|LINE|SEVERITY|ISSUE|FIX
One line per issue. Be concise."

FULL_PROMPT="${SYSTEM_PROMPT}

${DIFF_CONTENT}"

# Build JSON payload — use Python to properly escape prompt content
python3 -c "
import json, sys
payload = {
    'model': sys.argv[1],
    'prompt': sys.argv[2],
    'stream': True,
    'options': {'num_predict': 400}
}
with open('/tmp/review_payload.json', 'w') as f:
    json.dump(payload, f)
" "${REVIEW_MODEL}" "${FULL_PROMPT}"

log "Payload written. Starting curl | jq stream..."

# Stream: curl delivers chunks, jq --unbuffered flushes each .response token immediately.
# timeout 120 covers the whole pipeline.
timeout 120 bash -c '
  curl -s -N -m 110 \
    -X POST http://localhost:11434/api/generate \
    -H "Content-Type: application/json" \
    --data @/tmp/review_payload.json | \
  jq -rj --unbuffered ".response // empty"
' > /tmp/review_partial.txt 2>&1 || {
  log "Review timed out at 120s; using partial output."
}
log "Streaming done. Output: $(wc -c < /tmp/review_partial.txt 2>/dev/null || echo 0) bytes"

REVIEW_TEXT=$(cat /tmp/review_partial.txt 2>/dev/null || echo "")
if [[ -z "$REVIEW_TEXT" ]]; then
  REVIEW_TEXT="No review output generated (model may be too slow for CPU inference)."
fi

# --- Write output ---
printf '%s\n' "$REVIEW_TEXT" > "$REVIEW_OUTPUT"
log "Review written to ${REVIEW_OUTPUT}"

echo ""
echo "========== CODE REVIEW =========="
echo "$REVIEW_TEXT"
echo "================================="

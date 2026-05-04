#!/usr/bin/env bash
set -euo pipefail

REVIEW_OUTPUT="${REVIEW_OUTPUT:-/tmp/review.txt}"
DIFF_FILE="/tmp/pr.diff"
# Use COPILOT_MODEL env var if set; fall back to pre-baked default
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

# --- Verify model is present (no-op if pre-baked) ---
log "Verifying model ${BASE_MODEL} is present..."
ollama pull "${BASE_MODEL}" || log "Pull skipped or failed — model should already be in image."

# --- Create a context-limited model to speed up CPU inference ---
# Copilot CLI's built-in system prompt alone exceeds 10,000 tokens, making it
# incompatible with small context windows. We call the Ollama API directly to
# keep the total prompt small (~400 tokens) and inference fast (~30s on CPU).
log "Creating context-limited model '${REVIEW_MODEL}' (num_ctx 512)..."
cat > /tmp/Modelfile <<EOF
FROM ${BASE_MODEL}
PARAMETER num_ctx 512
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

# --- Authenticate gh if token provided ---
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  log "GITHUB_TOKEN detected; configuring gh auth..."
  echo "$GITHUB_TOKEN" | gh auth login --with-token 2>/dev/null || true
fi

# --- Truncate diff ---
MAX_DIFF_CHARS=600
DIFF_CONTENT=$(head -c "${MAX_DIFF_CHARS}" "${DIFF_FILE}")
DIFF_LEN=$(wc -c < "${DIFF_FILE}")
if [[ "${DIFF_LEN}" -gt "${MAX_DIFF_CHARS}" ]]; then
  DIFF_CONTENT="${DIFF_CONTENT}
[... diff truncated at ${MAX_DIFF_CHARS} chars ...]"
  log "Diff truncated from ${DIFF_LEN} chars to ${MAX_DIFF_CHARS} chars."
fi

# --- Run code review via Ollama API directly ---
log "Running code review (stream:true, num_predict:200, timeout 480s)..."

SYSTEM_PROMPT="You are a strict code reviewer. Check for ALL of the following:
1. TYPOS: misspelled identifiers, strings, comments (e.g. recieve->receive, lenght->length)
2. LOGIC: off-by-one, missing null/zero checks, wrong operators (- instead of +), unchecked errors
3. COMMENT: docstring/comment says one thing but code does another

For each issue output exactly one line:
FILE|LINE|SEVERITY|ISSUE|FIX
SEVERITY: ERROR, WARNING, or INFO. Output ONLY these lines, nothing else."

FULL_PROMPT="${SYSTEM_PROMPT}

DIFF:
${DIFF_CONTENT}

REVIEW:"

# Build JSON payload — use jq to properly escape prompt content
jq -n \
  --arg model "${REVIEW_MODEL}" \
  --arg prompt "${FULL_PROMPT}" \
  '{model: $model, prompt: $prompt, stream: true, options: {num_predict: 200, temperature: 0.1}}' \
  > /tmp/review_payload.json

log "Payload written."

# --- Main review request: stream:true ---
log "Starting review (stream:true, num_predict:200, timeout 480s)..."
CURL_EXIT=0
curl -s -N -m 480 \
  -X POST http://localhost:11434/api/generate \
  -H 'Content-Type: application/json' \
  --data @/tmp/review_payload.json \
  > /tmp/raw_stream.ndjson 2>/tmp/curl_err.txt || CURL_EXIT=$?

log "Review curl exit: ${CURL_EXIT}, size: $(wc -c < /tmp/raw_stream.ndjson) bytes"
[[ -s /tmp/curl_err.txt ]] && log "curl stderr: $(cat /tmp/curl_err.txt)"

# Extract .response tokens from the NDJSON stream
python3 - << 'PYEOF' > /tmp/review_partial.txt
import json, sys
try:
    with open('/tmp/raw_stream.ndjson', 'rb') as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
                t = d.get('response', '')
                if t:
                    sys.stdout.write(t)
            except json.JSONDecodeError:
                pass
except Exception as e:
    sys.stderr.write(f'[parse] error: {e}\n')
PYEOF

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

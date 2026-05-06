# Stage 1: builder
FROM golang:1.24-alpine AS builder

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /reviewer ./cmd/reviewer/

# Stage 2: runtime
FROM ollama/ollama:latest

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /reviewer /reviewer

# Install Copilot CLI via bundler
# The Go SDK requires CLI binary to be available in PATH
# Using curl to fetch the latest CLI release for Linux
RUN set -e; \
    COPILOT_CLI_VERSION="1.0.41"; \
    echo "[DEBUG] Installing Copilot CLI v${COPILOT_CLI_VERSION}..." && \
    curl -sL "https://github.com/github/copilot-cli/releases/download/v${COPILOT_CLI_VERSION}/copilot-linux-x64.tar.gz" | tar xz -C /usr/local/bin && \
    chmod +x /usr/local/bin/copilot && \
    echo "[DEBUG] CLI installation complete. Verifying..." && \
    ls -lh /usr/local/bin/copilot && \
    file /usr/local/bin/copilot && \
    /usr/local/bin/copilot --version && \
    echo "[DEBUG] CLI verification passed"

# Pre-bake the model during image build so CI never needs internet access at runtime.
RUN ollama serve & \
    SERVER_PID=$! && \
    echo "Waiting for ollama to start..." && \
    for i in $(seq 1 60); do \
      if curl -sf http://localhost:11434/api/tags > /dev/null 2>&1; then \
        echo "Ollama ready"; break; \
      fi; \
      sleep 1; \
    done && \
    ollama pull qwen2.5-coder:1.5b && \
    kill $SERVER_PID || true

ENV COPILOT_MODEL=qwen2.5-coder:1.5b
ENV COPILOT_OFFLINE=true
ENV OLLAMA_HOST=0.0.0.0

ENTRYPOINT ["/reviewer"]

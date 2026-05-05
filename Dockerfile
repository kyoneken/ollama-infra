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
      curl \
      ca-certificates \
      git && \
    rm -rf /var/lib/apt/lists/*

# Install GitHub CLI and gh-copilot extension
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y --no-install-recommends gh && \
    rm -rf /var/lib/apt/lists/* && \
    gh extension install github/gh-copilot

COPY --from=builder /reviewer /reviewer

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

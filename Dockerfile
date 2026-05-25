# Build stage
FROM golang:1.22-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /moe-sticker-bot ./cmd/moe-sticker-bot/main.go

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    imagemagick \
    libarchive-tools \
    ffmpeg \
    curl \
    gifsicle \
    python3 \
    python3-pip \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install python dependencies for the tools
RUN pip3 install --break-system-packages rlottie-python emoji

COPY tools/msb_emoji.py /usr/local/bin/msb_emoji.py
COPY tools/msb_kakao_decrypt.py /usr/local/bin/msb_kakao_decrypt.py
COPY tools/msb_rlottie.py /usr/local/bin/msb_rlottie.py
RUN chmod +x /usr/local/bin/msb_emoji.py /usr/local/bin/msb_kakao_decrypt.py /usr/local/bin/msb_rlottie.py

COPY --from=builder /moe-sticker-bot /usr/local/bin/moe-sticker-bot

VOLUME ["/data"]

ENTRYPOINT ["sh", "-c", "moe-sticker-bot --data_dir=/data --log_level=info --bot_token=$BOT_TOKEN"]
# Stage 1: Build React WebApp
FROM node:18-bookworm-slim AS webapp-builder

WORKDIR /webapp
COPY web/webapp3/package.json web/webapp3/package-lock.json ./
RUN npm ci
COPY web/webapp3/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.22-bookworm AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /moe-sticker-bot ./cmd/moe-sticker-bot/main.go

# Stage 3: Runtime
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
    nginx \
    supervisor \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --break-system-packages rlottie-python emoji

COPY tools/msb_emoji.py /usr/local/bin/msb_emoji.py
COPY tools/msb_kakao_decrypt.py /usr/local/bin/msb_kakao_decrypt.py
COPY tools/msb_rlottie.py /usr/local/bin/msb_rlottie.py
RUN chmod +x /usr/local/bin/msb_emoji.py /usr/local/bin/msb_kakao_decrypt.py /usr/local/bin/msb_rlottie.py

COPY --from=go-builder /moe-sticker-bot /usr/local/bin/moe-sticker-bot
COPY --from=webapp-builder /webapp/build /webapp/build

COPY web/nginx/fly.conf /etc/nginx/conf.d/default.conf
RUN rm -f /etc/nginx/sites-enabled/default

COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY start-bot.sh /usr/local/bin/start-bot.sh
RUN chmod +x /usr/local/bin/start-bot.sh

VOLUME ["/data"]

EXPOSE 8080

CMD ["supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]

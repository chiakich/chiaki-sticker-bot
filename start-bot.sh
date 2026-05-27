#!/bin/sh

# Create swap to handle memory spikes during video sticker conversion.
# fly.io Firecracker VMs have full Linux access so swapon works.
if [ ! -f /swapfile ]; then
    echo "Setting up 256MB swap..."
    fallocate -l 256M /swapfile && \
    chmod 600 /swapfile && \
    mkswap /swapfile && \
    swapon /swapfile && \
    echo "Swap enabled." || echo "Swap setup failed, continuing without swap."
fi

exec moe-sticker-bot \
    --data_dir=/data \
    --log_level=info \
    --bot_token=$BOT_TOKEN \
    --db_addr=$DB_ADDR \
    --db_user=$DB_USER \
    --db_pass=$DB_PASS \
    --admin_uid=207946916 \
    --webapp_url=https://chiaki-sticker-bot.fly.dev/webapp \
    --webapp_data_dir=/data/webapp \
    --webhook_url=https://chiaki-sticker-bot.fly.dev/webhook \
    --webhook_secret=$WEBHOOK_SECRET

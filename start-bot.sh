#!/bin/sh
nginx -g "daemon off;" &
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
    --webhook_url=$WEBHOOK_URL \
    --webhook_secret=$WEBHOOK_SECRET

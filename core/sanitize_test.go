package core

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestSanitizeLogTextMasksConfiguredBotToken(t *testing.T) {
	oldConf := msbconf
	msbconf.BotToken = "1234567890:abcdefghijklmnopqrstuvwxyzABCDEF"
	t.Cleanup(func() { msbconf = oldConf })

	raw := `telebot: Post "https://api.telegram.org/bot1234567890:abcdefghijklmnopqrstuvwxyzABCDEF/createNewStickerSet": context deadline exceeded`
	got := sanitizeLogText(raw)

	if strings.Contains(got, msbconf.BotToken) {
		t.Fatalf("sanitizeLogText leaked bot token: %q", got)
	}
	if !strings.Contains(got, "bot"+maskedSecret+"/createNewStickerSet") {
		t.Fatalf("sanitizeLogText did not preserve useful URL shape: %q", got)
	}
}

func TestSanitizeLogTextMasksTokenPatternWithoutConfiguredToken(t *testing.T) {
	oldConf := msbconf
	msbconf.BotToken = ""
	t.Cleanup(func() { msbconf = oldConf })

	raw := "token=1234567890:abcdefghijklmnopqrstuvwxyzABCDEF"
	got := sanitizeLogText(raw)

	if strings.Contains(got, "1234567890:abcdefghijklmnopqrstuvwxyzABCDEF") {
		t.Fatalf("sanitizeLogText leaked token-like value: %q", got)
	}
	if got != "token="+maskedSecret {
		t.Fatalf("sanitizeLogText = %q, want token mask", got)
	}
}

func TestSanitizingFormatterMasksMessagesAndFields(t *testing.T) {
	oldConf := msbconf
	msbconf.BotToken = "1234567890:abcdefghijklmnopqrstuvwxyzABCDEF"
	t.Cleanup(func() { msbconf = oldConf })

	var out bytes.Buffer
	logger := log.New()
	logger.SetOutput(&out)
	logger.SetFormatter(sanitizingFormatter{formatter: &log.TextFormatter{DisableColors: true}})

	logger.WithError(errors.New("Post https://api.telegram.org/bot" + msbconf.BotToken + "/sendMessage")).Warn("failed " + msbconf.BotToken)
	got := out.String()

	if strings.Contains(got, msbconf.BotToken) {
		t.Fatalf("sanitizingFormatter leaked bot token: %q", got)
	}
	if !strings.Contains(got, maskedSecret) {
		t.Fatalf("sanitizingFormatter did not include mask: %q", got)
	}
}

func TestRetryableTelegramWriteErrorIncludesDeadlineExceeded(t *testing.T) {
	err := errors.New(`telebot: Post "https://api.telegram.org/bot1234567890:abcdefghijklmnopqrstuvwxyzABCDEF/createNewStickerSet": context deadline exceeded`)

	if !isRetryableTelegramWriteError(err) {
		t.Fatal("expected Telegram context deadline exceeded error to be retryable")
	}
	if !isRetryableTelegramWriteError(context.DeadlineExceeded) {
		t.Fatal("expected context.DeadlineExceeded to be retryable")
	}
}

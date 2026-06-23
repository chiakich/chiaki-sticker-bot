package core

import (
	"fmt"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

const maskedSecret = "***"

var telegramBotTokenPattern = regexp.MustCompile(`\b\d{6,}:[A-Za-z0-9_-]{20,}\b`)

type sanitizingFormatter struct {
	formatter log.Formatter
}

func (f sanitizingFormatter) Format(entry *log.Entry) ([]byte, error) {
	sanitized := *entry
	sanitized.Message = sanitizeLogText(entry.Message)
	if len(entry.Data) > 0 {
		sanitized.Data = make(log.Fields, len(entry.Data))
		for key, value := range entry.Data {
			sanitized.Data[key] = sanitizeLogValue(value)
		}
	}
	return f.formatter.Format(&sanitized)
}

func sanitizeLogValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return sanitizeLogText(v)
	case error:
		return sanitizeLogText(v.Error())
	default:
		text := fmt.Sprint(v)
		clean := sanitizeLogText(text)
		if clean != text {
			return clean
		}
		return value
	}
}

func sanitizeLogText(text string) string {
	if text == "" {
		return text
	}
	if msbconf.BotToken != "" {
		text = strings.ReplaceAll(text, msbconf.BotToken, maskedSecret)
	}
	return telegramBotTokenPattern.ReplaceAllString(text, maskedSecret)
}

func sanitizeErrorText(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeLogText(err.Error())
}

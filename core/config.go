package core

var msbconf ConfigTemplate

type ConfigTemplate struct {
	AdminUid int64
	DataDir  string
	LogLevel string
	// UseDB            bool
	BotToken string
	// WebApp    bool
	WebappUrl         string
	WebappDataDir     string
	DbAddr            string
	DbUser            string
	DbPass            string
	WebhookPublicUrl  string // e.g. https://chiaki-sticker-bot.fly.dev/webhook
	WebhookSecretToken string
	// BotApiAddr       string
	// BotApiDir        string
}

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BotToken     string
	OwnerID      string
	Prefix       string
	WebhookHits  string
	WebhookBanned string
	WebhookUnbanned string
	MaxRetries   int
	Threads      int
	ProxyType    int // 1=HTTP 2=SOCKS4 3=SOCKS5 4=None 5=AutoScrape

	// Captures
	HypixelName      bool
	HypixelLevel     bool
	HypixelFirstLogin bool
	HypixelLastLogin bool
	OptifineCapture  bool
	MCCapes          bool
	EmailAccess      bool
	HypixelSBCoins   bool
	HypixelBWStars   bool
	HypixelBan       bool
	NameChange       bool
	LastChanged      bool
	DonutCheck       bool
	PaymentMethods   bool
	SaveCookies      bool
	ResultsChannelID  string

	// Auto
	SetName    bool
	CustomName string
	SetSkin    bool
	SkinURL    string

	// Scraper
	ProxySpeedTest bool
}

var defaultConfig = `# SilentRoot Discord Bot Config
[Bot]
bot_token = YOUR_BOT_TOKEN_HERE
owner_id = YOUR_DISCORD_USER_ID_HERE
prefix = $

[Webhooks]
webhook_hits = 
webhook_banned = 
webhook_unbanned = 

[Settings]
max_retries = 3
threads = 50
proxy_type = 4

[Captures]
hypixel_name = true
hypixel_level = true
hypixel_first_login = true
hypixel_last_login = true
optifine_cape = true
mc_capes = true
email_access = true
hypixel_sb_coins = true
hypixel_bw_stars = true
hypixel_ban = true
name_change = true
last_changed = true
donut_check = true
payment_methods = true
save_cookies = false
results_channel_id = 

[Auto]
set_name = false
custom_name = SilentRoot
set_skin = false
skin_url = 

[Scraper]
proxy_speed_test = false
`

func loadConfig() *Config {
	if _, err := os.Stat("config.ini"); os.IsNotExist(err) {
		_ = os.WriteFile("config.ini", []byte(defaultConfig), 0644)
		fmt.Println("Created default config.ini — please fill in your bot token and owner ID, then restart.")
		os.Exit(0)
	}

	raw := parseINI("config.ini")
	cfg := &Config{
		BotToken:        get(raw, "bot_token", ""),
		OwnerID:         get(raw, "owner_id", ""),
		Prefix:          get(raw, "prefix", "$"),
		WebhookHits:     get(raw, "webhook_hits", ""),
		WebhookBanned:   get(raw, "webhook_banned", ""),
		WebhookUnbanned: get(raw, "webhook_unbanned", ""),
		MaxRetries:      getInt(raw, "max_retries", 3),
		Threads:         getInt(raw, "threads", 50),
		ProxyType:       getInt(raw, "proxy_type", 4),

		HypixelName:       getBool(raw, "hypixel_name", true),
		HypixelLevel:      getBool(raw, "hypixel_level", true),
		HypixelFirstLogin: getBool(raw, "hypixel_first_login", true),
		HypixelLastLogin:  getBool(raw, "hypixel_last_login", true),
		OptifineCapture:   getBool(raw, "optifine_cape", true),
		MCCapes:           getBool(raw, "mc_capes", true),
		EmailAccess:       getBool(raw, "email_access", true),
		HypixelSBCoins:    getBool(raw, "hypixel_sb_coins", true),
		HypixelBWStars:    getBool(raw, "hypixel_bw_stars", true),
		HypixelBan:        getBool(raw, "hypixel_ban", true),
		NameChange:        getBool(raw, "name_change", true),
		LastChanged:       getBool(raw, "last_changed", true),
		DonutCheck:        getBool(raw, "donut_check", true),
		PaymentMethods:    getBool(raw, "payment_methods", false),
		SaveCookies:       getBool(raw, "save_cookies", false),
		ResultsChannelID:  get(raw, "results_channel_id", ""),

		SetName:    getBool(raw, "set_name", false),
		CustomName: get(raw, "custom_name", "SilentRoot"),
		SetSkin:    getBool(raw, "set_skin", false),
		SkinURL:    get(raw, "skin_url", ""),

		ProxySpeedTest: getBool(raw, "proxy_speed_test", false),
	}
	return cfg
}

func parseINI(filename string) map[string]string {
	result := make(map[string]string)
	f, err := os.Open(filename)
	if err != nil {
		return result
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			result[key] = val
		}
	}
	return result
}

func get(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return def
}

func getInt(m map[string]string, key string, def int) int {
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getBool(m map[string]string, key string, def bool) bool {
	if v, ok := m[key]; ok {
		v = strings.ToLower(v)
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}

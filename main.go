package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

func main() {
	cfg := loadConfig()

	if cfg.BotToken == "" || cfg.BotToken == "YOUR_BOT_TOKEN_HERE" {
		fmt.Println("❌ No bot token set. Please edit config.ini and set your bot token.")
		os.Exit(1)
	}
	if cfg.OwnerID == "" || cfg.OwnerID == "YOUR_DISCORD_USER_ID_HERE" {
		fmt.Println("❌ No owner ID set. Please edit config.ini and set your Discord user ID.")
		os.Exit(1)
	}

	// Set proxy type from config
	gProxies.SetType(cfg.ProxyType)

	// Start auto-scrape if configured
	if cfg.ProxyType == 5 {
		fmt.Println("[Proxy] Starting auto-scrape...")
		go gProxies.AutoScrape()
	}

	// Create Discord session
	dg, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		log.Fatal("Error creating Discord session:", err)
	}

	bot := NewBot(cfg, dg)
	dg.AddHandler(bot.messageCreate)

	// Intents: read guild messages + message content
	dg.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	if err := dg.Open(); err != nil {
		log.Fatal("Error opening Discord connection:", err)
	}
	defer dg.Close()

	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║   SilentRoot MC Checker — Go Bot     ║")
	fmt.Println("╠══════════════════════════════════════╣")
	fmt.Printf("║  Prefix  : %-27s║\n", cfg.Prefix)
	fmt.Printf("║  Threads : %-27d║\n", cfg.Threads)
	fmt.Printf("║  Proxy   : %-27s║\n", proxyTypeName(cfg.ProxyType))
	fmt.Println("╠══════════════════════════════════════╣")
	fmt.Println("║  Bot is online! Press Ctrl+C to stop ║")
	fmt.Println("╚══════════════════════════════════════╝")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	fmt.Println("\nShutting down...")
	if checker.IsRunning() {
		fmt.Println("Stopping checker...")
		checker.Stop()
	}
}

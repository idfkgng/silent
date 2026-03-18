# SilentRoot MC Checker — Discord Bot (Go Edition)

High-performance Go rewrite of SilentRoot MC Checker, running entirely as a Discord bot.

## Features
- ✅ Full Microsoft → Xbox → Minecraft auth chain
- ✅ Concurrent checking with goroutines (much faster than Python's threading)
- ✅ All original result categories: Hits, Bad, SFA/MFA, 2FA, XGPU/XGP, Other, ValidMail
- ✅ Results saved to timestamped `results/` folders
- ✅ Discord webhook on every hit with rich embed
- ✅ Live `$cui` status display matching the original UI
- ✅ Proxy support: HTTP, SOCKS4, SOCKS5, Proxyless, Auto-Scrape

## Requirements

- Go 1.21+ — download from https://go.dev/dl/
- A Discord Bot token — create at https://discord.com/developers/applications
- Your Discord user ID (right-click your name → Copy User ID with Developer Mode on)

## Setup

1. **Install Go** from https://go.dev/dl/

2. **Create a Discord Bot**
   - Go to https://discord.com/developers/applications
   - New Application → Bot → Reset Token → copy token
   - Under "Privileged Gateway Intents" enable **Message Content Intent**
   - Invite the bot with permissions: `Send Messages`, `Read Message History`, `Attach Files`

3. **Configure**
   ```
   cp config.ini.example config.ini  # (or run the bot once to generate it)
   ```
   Edit `config.ini`:
   ```ini
   [Bot]
   bot_token = YOUR_BOT_TOKEN
   owner_id  = YOUR_DISCORD_USER_ID
   prefix    = $
   
   [Settings]
   threads    = 50
   proxy_type = 4     # 4 = proxyless
   ```

4. **Build & Run**
   ```bash
   go mod tidy
   go build -o silentroot-bot .
   ./silentroot-bot
   ```
   Or run directly:
   ```bash
   go run .
   ```

## Commands

| Command | Description |
|---------|-------------|
| `$help` | Show all commands |
| `$auth <user>` | Authorize a user (owner only) |
| `$unauth <user>` | Remove authorization (owner only) |
| `$cui` | Show live checker status |
| `$check` + attachment | Start checker with a combo .txt file |
| `$stop` | Stop checker and show final results |
| `$uploadproxy` + attachment | Load a proxy file |
| `$changeproxytype <1-5>` | 1:HTTP 2:SOCKS4 3:SOCKS5 4:None 5:Auto-Scrape |

## Usage Example

1. Run the bot
2. In Discord, type `$help` to confirm it's online
3. Attach your combo file and type `$check`
4. Monitor with `$cui`
5. Type `$stop` when done (or wait for it to finish)

## Results

Results are saved to `results/YYYY-MM-DD_HH-MM-SS/`:
- `Hits.txt` — Full MC accounts
- `XboxGamePassUltimate.txt` — XGPU accounts
- `XboxGamePass.txt` — XGP accounts
- `Other.txt` — Bedrock/Legends/Dungeons
- `2fa.txt` — Two-factor protected
- `Valid_Mail.txt` — Valid Microsoft accounts without MC
- `Capture.txt` — Detailed capture info for hits

## Performance

Go goroutines are significantly lighter than Python threads. With 50 threads and good proxies you can expect 2-5x better CPM than the Python version. With 200+ threads and premium proxies, 10x+ is possible.

## Proxy Type Guide

| Type | Best for |
|------|----------|
| 1 (HTTP) | Fastest, most proxies available |
| 3 (SOCKS5) | More reliable, better for ban checks |
| 4 (None) | Testing only — will rate-limit fast |
| 5 (Auto-Scrape) | Automatic — scrapes public proxy lists |

For proxyless mode, keep threads at 5-10 to avoid Microsoft rate limits.

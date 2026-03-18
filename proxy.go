package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type ProxyManager struct {
	mu        sync.RWMutex
	proxies   []string
	proxyType int // 1=HTTP 2=SOCKS4 3=SOCKS5 4=None 5=AutoScrape
}

var gProxies = &ProxyManager{proxyType: 4}

func (pm *ProxyManager) SetType(t int) {
	pm.mu.Lock()
	pm.proxyType = t
	pm.mu.Unlock()
}

func (pm *ProxyManager) Load(lines []string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.proxies = nil
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			pm.proxies = append(pm.proxies, l)
		}
	}
}

func (pm *ProxyManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.proxies)
}

func (pm *ProxyManager) random() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if len(pm.proxies) == 0 {
		return ""
	}
	return pm.proxies[rand.Intn(len(pm.proxies))]
}

// MakeClient creates an *http.Client with the appropriate proxy transport.
func (pm *ProxyManager) MakeClient(timeout time.Duration) *http.Client {
	pm.mu.RLock()
	pt := pm.proxyType
	pm.mu.RUnlock()

	transport := makeTransport(pt, pm.random())
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // we handle redirects manually
		},
	}
}

func makeTransport(proxyType int, proxyAddr string) http.RoundTripper {
	base := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
	}

	if proxyAddr == "" || proxyType == 4 {
		return base
	}

	switch proxyType {
	case 1: // HTTP
		proxyURL, err := url.Parse("http://" + proxyAddr)
		if err == nil {
			base.Proxy = http.ProxyURL(proxyURL)
		}
	case 2: // SOCKS4 — golang.org/x/net/proxy doesn't support SOCKS4, use HTTP fallback
		proxyURL, err := url.Parse("socks4://" + proxyAddr)
		if err == nil {
			base.Proxy = http.ProxyURL(proxyURL)
		}
	case 3, 5: // SOCKS5 / auto scrape (mixed — try SOCKS5 format)
		if strings.Contains(proxyAddr, "://") {
			// already has scheme
			u, err := url.Parse(proxyAddr)
			if err == nil && u.Scheme == "socks5" {
				dialer, err := proxy.SOCKS5("tcp", u.Host, nil, proxy.Direct)
				if err == nil {
					base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialer.Dial(network, addr)
					}
				}
			}
		} else {
			dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
			if err == nil {
				base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				}
			}
		}
	}

	return base
}

// AutoScrape scrapes free proxies from public sources.
func (pm *ProxyManager) AutoScrape() {
	sources := []string{
		"https://api.proxyscrape.com/v3/free-proxy-list/get?request=getproxies&protocol=http&timeout=10000&proxy_format=ipport&format=text",
		"https://api.proxyscrape.com/v3/free-proxy-list/get?request=getproxies&protocol=socks5&timeout=10000&proxy_format=ipport&format=text",
		"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt",
		"https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt",
		"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
		"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt",
		"https://raw.githubusercontent.com/hookzof/socks5_list/master/proxy.txt",
		"https://raw.githubusercontent.com/ALIILAPRO/Proxy/main/http.txt",
		"https://raw.githubusercontent.com/ALIILAPRO/Proxy/main/socks5.txt",
		"https://raw.githubusercontent.com/roosterkid/openproxylist/main/SOCKS5_RAW.txt",
	}

	var collected []string
	var wg sync.WaitGroup
	var mu sync.Mutex

	client := &http.Client{Timeout: 10 * time.Second}

	for _, src := range sources {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			resp, err := client.Get(u)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			lines := strings.Split(string(body), "\n")
			mu.Lock()
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if isValidProxy(l) {
					collected = append(collected, l)
				}
			}
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	// Deduplicate
	seen := make(map[string]bool)
	var deduped []string
	for _, p := range collected {
		if !seen[p] {
			seen[p] = true
			deduped = append(deduped, p)
		}
	}

	pm.Load(deduped)
	fmt.Printf("[Proxy] Auto-scraped %d proxies\n", len(deduped))
}

func isValidProxy(p string) bool {
	parts := strings.Split(p, ":")
	if len(parts) != 2 {
		return false
	}
	ip := net.ParseIP(parts[0])
	if ip == nil {
		return false
	}
	// quick port check
	for _, c := range parts[1] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

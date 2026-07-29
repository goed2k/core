package goed2k

import (
	"sync"
	"time"
)

type downloadRateLimiter struct {
	mu       sync.Mutex
	rateBps  float64
	tokens   float64
	lastTick time.Time
}

func newDownloadRateLimiter(rateKB int) *downloadRateLimiter {
	limiter := &downloadRateLimiter{lastTick: time.Now()}
	limiter.setRateKBLocked(rateKB)
	if rateKB > 0 {
		limiter.tokens = 0
	}
	return limiter
}

func (l *downloadRateLimiter) setRateKB(rateKB int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.setRateKBLocked(rateKB)
}

func (l *downloadRateLimiter) setRateKBLocked(rateKB int) {
	if rateKB <= 0 {
		l.rateBps = 0
		return
	}
	l.rateBps = float64(rateKB) * 1024
	burst := l.rateBps
	if burst < 1024 {
		burst = 1024
	}
	if l.tokens == 0 || l.tokens > burst {
		l.tokens = burst
	}
}

func (l *downloadRateLimiter) wait(bytes int) {
	if l == nil || l.rateBps <= 0 || bytes <= 0 {
		return
	}
	need := float64(bytes)
	for {
		var sleepDuration time.Duration
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.lastTick).Seconds()
		if elapsed > 0 {
			l.tokens += elapsed * l.rateBps
			maxBurst := l.rateBps
			if maxBurst < 1024 {
				maxBurst = 1024
			}
			if l.tokens > maxBurst {
				l.tokens = maxBurst
			}
			l.lastTick = now
		}
		if l.tokens >= need {
			l.tokens -= need
			l.mu.Unlock()
			return
		}
		deficit := need - l.tokens
		l.tokens = 0
		sleepDuration = time.Duration(deficit/l.rateBps*float64(time.Second)) + time.Millisecond
		l.mu.Unlock()
		time.Sleep(sleepDuration)
	}
}

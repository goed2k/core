package goed2k

import (
	"testing"
	"time"

	"github.com/goed2k/core/protocol"
)

func TestDownloadRateLimiterDisabled(t *testing.T) {
	limiter := newDownloadRateLimiter(0)
	start := time.Now()
	limiter.wait(1024 * 1024)
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("expected no throttle when disabled, waited %v", elapsed)
	}
}

func TestDownloadRateLimiterThrottles(t *testing.T) {
	limiter := newDownloadRateLimiter(64)
	start := time.Now()
	limiter.wait(64 * 1024)
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected download throttle delay, got %v", elapsed)
	}
}

func TestSessionThrottleDownloadRespectsMaxDownloadRateKB(t *testing.T) {
	settings := NewSettings()
	settings.MaxDownloadRateKB = 32
	session := NewSession(settings)

	start := time.Now()
	session.ThrottleDownload(32 * 1024)
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected session download throttle, got %v", elapsed)
	}

	settings.MaxDownloadRateKB = 0
	session.ConfigureSession(settings)
	start = time.Now()
	session.ThrottleDownload(256 * 1024)
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("expected unlimited download after reconfigure, waited %v", elapsed)
	}
}

func TestTransferThrottleDownloadWriteDelegatesToSession(t *testing.T) {
	settings := NewSettings()
	settings.MaxDownloadRateKB = 48
	session := NewSession(settings)
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       BlockSize,
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	start := time.Now()
	transfer.throttleDownloadWrite(48 * 1024)
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Fatalf("expected transfer write throttle, got %v", elapsed)
	}
}

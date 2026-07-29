package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	ed2k "github.com/goed2k/core"
	"github.com/goed2k/core/internal/webapi"
)

// goed2k-web 是早期简易 REST 示例，已废弃。
// 请改用：
//   - 后端：https://github.com/goed2k/daemon （goed2kd，/api/v1 + WebSocket）
//   - 前端：https://github.com/goed2k/webui

func main() {
	log.Println("DEPRECATED: goed2k-web is deprecated; use github.com/goed2k/daemon and github.com/goed2k/webui instead")

	addr := envOr("GOED2K_WEB_ADDR", ":8080")
	outDir := envOr("GOED2K_WEB_OUTDIR", ".")
	user := os.Getenv("GOED2K_WEB_USER")
	pass := os.Getenv("GOED2K_WEB_PASS")

	settings := ed2k.NewSettings()
	client := ed2k.NewClient(settings)
	if err := client.Start(); err != nil {
		log.Fatalf("client start failed: %v", err)
	}
	defer client.Close()

	handler := webapi.NewHandler(client, outDir, user, pass)
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go func() {
		log.Printf("goed2k-web listening on %s (deprecated)", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("shutting down...")
	_ = server.Close()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

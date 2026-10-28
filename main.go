package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web
var embeddedWeb embed.FS

// skins/ holds the picture tokens: drop an image there and redeploy.
//
//go:embed skins
var embeddedSkins embed.FS

func main() {
	addr := flag.String("addr", ":8080", "адрес, на котором слушает сервер (:8080 — со всех интерфейсов)")
	publicURL := flag.String("public-url", "", "адрес игры из интернета (только для сообщения в консоли)")
	maxRooms := flag.Int("max-rooms", 200, "сколько комнат держать одновременно")
	maxPerIP := flag.Int("max-per-ip", 20, "сколько соединений разрешено с одного адреса (0 — без ограничения)")
	flag.Parse()

	web, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatal(err)
	}
	skins, err := fs.Sub(embeddedSkins, "skins")
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := NewHub(*maxRooms, *maxPerIP)
	go hub.Janitor(ctx)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           NewServer(hub, web, skins).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}

	_, port, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatalf("некорректный -addr: %v", err)
	}
	fmt.Printf("🎩 Монополия запущена\n  Здесь:  http://localhost:%s\n", port)
	if *publicURL != "" {
		fmt.Printf("  Снаружи: %s\n", *publicURL)
	}
	fmt.Println("Ctrl+C — остановить")

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		log.Fatal(err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		log.Print(err)
	}
}

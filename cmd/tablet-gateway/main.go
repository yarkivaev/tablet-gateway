package main

import (
	"context"
	"encoding/json"
	"github.com/yarkivaev/tablet-gateway/gateway"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	token := os.Getenv("TABLET_GATEWAY_TOKEN")
	if len(token) < 24 {
		log.Fatal("TABLET_GATEWAY_TOKEN must contain at least 24 characters")
	}
	path := os.Getenv("TABLET_GATEWAY_CONFIG")
	if path == "" {
		path = "/etc/tablet-gateway/tablets.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var list []gateway.Device
	if err = json.Unmarshal(data, &list); err != nil {
		log.Fatal(err)
	}
	devices := map[string]gateway.Device{}
	for _, d := range list {
		if d.ID == "" || d.Serial == "" || devices[d.ID].ID != "" {
			log.Fatal("invalid or duplicate tablet")
		}
		devices[d.ID] = d
	}
	server := os.Getenv("SCRCPY_SERVER")
	if server == "" {
		server = "/usr/share/tablet-gateway/scrcpy-server"
	}
	api := &gateway.HTTP{Devices: devices, Token: token, Manager: gateway.NewManager(gateway.AndroidFactory(devices, server)), PublicIP: os.Getenv("WEBRTC_PUBLIC_IP")}
	mux := http.NewServeMux()
	mux.Handle("/api/", api.Handler())
	mux.Handle("/healthz", api.Handler())
	mux.Handle("/", http.FileServer(http.Dir("web")))
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	httpServer := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		api.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdown)
	}()
	log.Printf("tablet gateway listening on %s (%d tablets)", addr, len(devices))
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

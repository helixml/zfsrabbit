package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"zfsrabbit/internal/config"
	"zfsrabbit/internal/server"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "/etc/zfsrabbit/config.yaml", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	srv.Stop()
}
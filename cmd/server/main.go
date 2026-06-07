package main

import (
	"log"
	"os"
	"time"

	"ai-gateway-mvp/internal/api"
	"ai-gateway-mvp/internal/proxy"
	"ai-gateway-mvp/internal/store"
)

func main() {
	dbPath := getenv("DB_PATH", "gateway.db")
	port := getenv("PORT", "8080")
	relayTimeout := api.EnvDurationSeconds("RELAY_TIMEOUT_SECONDS", 3*time.Second)

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	server := api.NewServer(st, proxy.NewMockProvider(), relayTimeout)
	if err := server.Router().Run(":" + port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

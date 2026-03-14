package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/PCS-Indonesia/pcspubsub/pubsubclient"
	"github.com/joho/godotenv"
)

// ReceiverConfig holds configuration for a single Pub/Sub receiver
type ReceiverConfig struct {
	Name            string
	ProjectID       string
	CredentialsPath string
	SubscriptionID  string
	WebhookURL      string
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using system environment variables")
	}
}

func main() {
	// MTR Configuration
	mtrConfig := ReceiverConfig{
		Name:            "MTR",
		ProjectID:       getEnv("PROJECT_ID", ""),
		CredentialsPath: getEnv("GOOGLE_APPLICATION_CREDENTIALS", "./service-account.json"),
		SubscriptionID:  getEnv("SUBSCRIPTION_ID", ""),
		WebhookURL:      getEnv("GOOGLE_CHAT_WEBHOOK", ""),
	}

	// SB Configuration
	sbConfig := ReceiverConfig{
		Name:            "SB",
		ProjectID:       getEnv("SB_PROJECT_ID", ""),
		CredentialsPath: getEnv("SB_GOOGLE_APPLICATION_CREDENTIALS", "./sb-service-account.json"),
		SubscriptionID:  getEnv("SB_SUBSCRIPTION_ID", ""),
		WebhookURL:      getEnv("SB_GOOGLE_CHAT_WEBHOOK", ""),
	}

	// Setup signal handling for graceful shutdown
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Collect all configs
	configs := []ReceiverConfig{mtrConfig, sbConfig}

	for _, cfg := range configs {
		// Skip if not configured
		if cfg.ProjectID == "" || cfg.SubscriptionID == "" {
			log.Printf("[%s] Skipped - not configured", cfg.Name)
			continue
		}

		client, err := pubsubclient.NewPubSubClient(ctx, cfg.ProjectID, cfg.CredentialsPath, 10)
		if err != nil {
			log.Printf("[%s] Failed to create Pub/Sub client: %v", cfg.Name, err)
			continue
		}

		log.Printf("[%s] ✅ Connected | Project: %s | Subscription: %s", cfg.Name, cfg.ProjectID, cfg.SubscriptionID)

		wg.Add(1)
		go startReceiver(&wg, client, cfg)
	}

	<-sigchan
	log.Println("Shutdown signal received, stopping receivers...")
	cancel()
	wg.Wait()
	log.Println("System shutdown gracefully")
}

func startReceiver(wg *sync.WaitGroup, client *pubsubclient.PubSubClient, cfg ReceiverConfig) {
	defer wg.Done()

	err := client.ReceiveMessages(cfg.SubscriptionID, func(ctx context.Context, msg pubsubclient.CommandMessage) error {
		return messageHandler(ctx, msg, cfg.Name, cfg.WebhookURL)
	})
	if err != nil {
		log.Printf("[%s] Error receiving messages: %v", cfg.Name, err)
	}

	log.Printf("[%s] Receiver stopped", cfg.Name)
}

func messageHandler(ctx context.Context, msg pubsubclient.CommandMessage, name string, webhookURL string) error {
	log.Printf("[%s] ========================================", name)
	log.Printf("[%s]   Command: %s", name, msg.Command)
	log.Printf("[%s]   Detail: %s", name, msg.Detail)
	log.Printf("[%s]   Payload: %s", name, msg.Payload)
	log.Printf("[%s] ========================================", name)

	if webhookURL != "" {
		if err := sendToGoogleChat(webhookURL, msg); err != nil {
			log.Printf("[%s] Failed to send to Google Chat: %v", name, err)
			return err
		}
	}

	return nil
}

func sendToGoogleChat(webhookURL string, msg pubsubclient.CommandMessage) error {
	msgData := map[string]interface{}{
		"command": msg.Command,
		"payload": msg.Payload,
		"id":      msg.ID,
		"detail":  msg.Detail,
	}

	prettyJSON, err := json.MarshalIndent(msgData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}

	wib, _ := time.LoadLocation("Asia/Jakarta")
	timestamp := fmt.Sprintf("timestamp: %s", time.Now().In(wib).Format("2006-01-02 15:04:05.000"))
	fullText := fmt.Sprintf("%s\n\n%s", timestamp, string(prettyJSON))

	// Google Chat limit is 4096 chars, truncate if needed
	const maxLen = 4090
	if len(fullText) > maxLen {
		fullText = fullText[:maxLen] + "\n..."
	}

	return postToWebhook(webhookURL, fullText)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// postToWebhook sends a single message to Google Chat webhook with retry on rate limit
func postToWebhook(webhookURL string, text string) error {
	payload := map[string]interface{}{
		"text": text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		// Rate limited, retry after delay
		if resp.StatusCode == 429 && attempt < maxRetries {
			delay := time.Duration(attempt+1) * 3 * time.Second
			log.Printf("Rate limited, retrying in %v... (attempt %d/%d)", delay, attempt+1, maxRetries)
			time.Sleep(delay)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

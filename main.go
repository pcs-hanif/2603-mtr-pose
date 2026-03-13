package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using system environment variables")
	}
}

func main() {
	// Configuration
	projectID := getEnv("PROJECT_ID", "silicon-guru-351910")
	credentialsPath := getEnv("GOOGLE_APPLICATION_CREDENTIALS", "./service-account.json")
	subscriptionID := getEnv("SUBSCRIPTION_ID", "dev-cashloy.membership-sub")
	webhookURL := getEnv("GOOGLE_CHAT_WEBHOOK", "")

	// Setup signal handling for graceful shutdown
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, os.Interrupt, syscall.SIGTERM)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Pub/Sub client
	// Parameters: context, projectID, credentialsPath, maxConcurrent
	client, err := pubsubclient.NewPubSubClient(ctx, projectID, credentialsPath, 10)
	if err != nil {
		log.Fatalf("Failed to create Pub/Sub client: %v", err)
	}

	log.Printf("Starting Pub/Sub receiver...")
	log.Printf("Project ID: %s", projectID)
	log.Printf("Subscription ID: %s", subscriptionID)

	// WaitGroup to wait for goroutine to finish
	var wg sync.WaitGroup
	wg.Add(1)

	// Start receiving messages in a goroutine
	go func() {
		defer wg.Done()

		err = client.ReceiveMessages(subscriptionID, func(ctx context.Context, msg pubsubclient.CommandMessage) error {
			return messageHandler(ctx, msg, webhookURL)
		})
		if err != nil {
			log.Printf("Error receiving messages from %s: %v", subscriptionID, err)
		}

		log.Println("Message receiver stopped")
	}()

	// Wait for interrupt signal (Ctrl+C)
	<-sigchan
	log.Println("Shutdown signal received, stopping receiver...")

	// Cancel context to stop receiving messages
	cancel()

	// Wait for goroutine to finish
	wg.Wait()

	log.Println("System shutdown gracefully")
}

// messageHandler is called for each received message
func messageHandler(ctx context.Context, msg pubsubclient.CommandMessage, webhookURL string) error {
	log.Println("========================================")
	log.Printf("  Command: %s", msg.Command)
	log.Printf("  Detail: %s", msg.Detail)
	log.Printf("  Payload: %s", msg.Payload)
	log.Println("========================================")

	// Send to Google Chat webhook
	if webhookURL != "" {
		if err := sendToGoogleChat(webhookURL, msg); err != nil {
			log.Printf("Failed to send to Google Chat: %v", err)
			return err
		}
	}

	return nil
}

// sendToGoogleChat sends message to Google Chat webhook
func sendToGoogleChat(webhookURL string, msg pubsubclient.CommandMessage) error {
	// Create JSON structure
	msgData := map[string]interface{}{
		"command": msg.Command,
		"payload": msg.Payload,
		"id":      msg.ID,
		"detail":  msg.Detail,
	}

	// Format as pretty JSON
	prettyJSON, err := json.MarshalIndent(msgData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}

	// Format message for Google Chat with timestamp and pretty JSON
	text := fmt.Sprintf("timestamp: %s\n\n%s",
		time.Now().Format("2006-01-02 15:04:05.000"),
		string(prettyJSON))

	payload := map[string]interface{}{
		"text": text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	log.Println("Successfully sent message to Google Chat")
	return nil
}

// getEnv gets environment variable with default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

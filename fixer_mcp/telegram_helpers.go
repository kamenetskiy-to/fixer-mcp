package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func resolveTelegramOperatorConfigFromEnv() (string, string, string, error) {
	botToken := strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_BOT_TOKEN"))
	if botToken == "" {
		return "", "", "", fmt.Errorf("FIXER_MCP_TELEGRAM_BOT_TOKEN is not set")
	}

	chatID := strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_CHAT_ID"))
	if chatID == "" {
		return "", "", "", fmt.Errorf("FIXER_MCP_TELEGRAM_CHAT_ID is not set")
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FIXER_MCP_TELEGRAM_API_BASE_URL")), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultTelegramAPIBaseURL
	}

	return botToken, chatID, apiBaseURL, nil
}

func sendTelegramText(ctx context.Context, botToken, chatID, apiBaseURL, text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return fmt.Errorf("failed to encode telegram payload: %v", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", apiBaseURL, botToken),
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("failed to build telegram request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("failed to read telegram response: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram send failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var telegramResp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if len(body) > 0 && json.Unmarshal(body, &telegramResp) == nil && !telegramResp.OK {
		description := strings.TrimSpace(telegramResp.Description)
		if description == "" {
			description = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("telegram send failed: %s", description)
	}

	return nil
}

func renderTelegramOperatorNotification(
	projectName string,
	projectID int,
	source string,
	status string,
	summary string,
	sessionID int,
	runState string,
	details string,
) string {
	lines := []string{
		"Fixer MCP: уведомление оператору",
		fmt.Sprintf("Проект: %s (#%d)", projectName, projectID),
		fmt.Sprintf("Источник: %s", source),
		fmt.Sprintf("Статус: %s", status),
	}
	if sessionID > 0 {
		lines = append(lines, fmt.Sprintf("Сессия: %d", sessionID))
	}
	if runState != "" {
		lines = append(lines, fmt.Sprintf("Прогон: %s", runState))
	}
	if summary != "" {
		lines = append(lines, fmt.Sprintf("Сводка: %s", summary))
	}
	if details != "" {
		lines = append(lines, fmt.Sprintf("Детали: %s", details))
	}
	return strings.Join(lines, "\n")
}

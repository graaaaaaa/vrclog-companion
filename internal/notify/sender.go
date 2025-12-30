package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/graaaaa/vrclog-companion/internal/config"
)

// SendResult indicates the outcome of a send attempt.
type SendResult int

const (
	// SendOK indicates successful delivery.
	SendOK SendResult = iota
	// SendRetryable indicates a transient error (429, network error).
	SendRetryable
	// SendFatal indicates a permanent error (401/403, invalid webhook).
	SendFatal
)

// Sender abstracts Discord webhook sending for testing.
type Sender interface {
	// Send sends a payload to Discord.
	// Returns the result and retry-after duration (for 429 responses).
	Send(ctx context.Context, payload DiscordPayload) (SendResult, time.Duration)
}

// DiscordSender sends webhooks to Discord.
type DiscordSender struct {
	webhookURL config.Secret
	client     *http.Client
	logger     *slog.Logger
}

// SenderOption configures a DiscordSender.
type SenderOption func(*DiscordSender)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) SenderOption {
	return func(s *DiscordSender) { s.client = client }
}

// WithSenderLogger sets the logger.
func WithSenderLogger(logger *slog.Logger) SenderOption {
	return func(s *DiscordSender) { s.logger = logger }
}

// NewDiscordSender creates a new Discord sender.
// The webhookURL is stored as a Secret and will appear as [REDACTED] in logs.
func NewDiscordSender(webhookURL config.Secret, opts ...SenderOption) *DiscordSender {
	s := &DiscordSender{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Send implements Sender.
func (s *DiscordSender) Send(ctx context.Context, payload DiscordPayload) (SendResult, time.Duration) {
	if s.webhookURL.IsEmpty() {
		s.logger.Warn("Discord webhook URL not configured")
		return SendFatal, 0
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("failed to marshal Discord payload", "error", err)
		return SendFatal, 0
	}

	// Note: webhookURL.Value() gets the actual URL for the request
	// but webhookURL itself logs as [REDACTED]
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL.Value(), bytes.NewReader(body))
	if err != nil {
		s.logger.Error("failed to create request", "error", err)
		return SendFatal, 0
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("Discord request failed", "error", err)
		return SendRetryable, 0
	}
	defer resp.Body.Close()

	// Drain body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		s.logger.Debug("Discord notification sent", "status", resp.StatusCode)
		return SendOK, 0

	case resp.StatusCode == 429:
		// Rate limited - check Retry-After header
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		s.logger.Warn("Discord rate limited", "retry_after", retryAfter)
		return SendRetryable, retryAfter

	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// 4xx (except 429) = configuration error (invalid URL, auth failed, etc.)
		// These are fatal and won't recover with retry
		s.logger.Error("Discord webhook client error",
			"status", resp.StatusCode,
			"webhook_url", s.webhookURL, // logs as [REDACTED]
		)
		return SendFatal, 0

	case resp.StatusCode >= 500:
		// 5xx = server error, retryable
		s.logger.Warn("Discord server error", "status", resp.StatusCode)
		return SendRetryable, 0

	default:
		s.logger.Warn("Discord request failed", "status", resp.StatusCode)
		return SendRetryable, 0
	}
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	// Discord typically sends seconds as an integer
	if secs, err := strconv.Atoi(header); err == nil {
		return time.Duration(secs) * time.Second
	}
	// Also try parsing as float (Discord sometimes sends decimals)
	if secs, err := strconv.ParseFloat(header, 64); err == nil {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

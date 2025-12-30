package notify

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// BackoffConfig configures exponential backoff.
type BackoffConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	JitterFactor float64 // 0.0 to 1.0
}

// DefaultBackoffConfig provides sensible defaults for Discord API.
var DefaultBackoffConfig = BackoffConfig{
	InitialDelay: 1 * time.Second,
	MaxDelay:     5 * time.Minute,
	Multiplier:   2.0,
	JitterFactor: 0.2,
}

// BackoffCalculator calculates exponential backoff with jitter.
// It uses its own RNG instance for thread safety and test determinism.
type BackoffCalculator struct {
	cfg BackoffConfig
	rng *rand.Rand
	mu  sync.Mutex
}

// NewBackoffCalculator creates a new BackoffCalculator with a random seed.
func NewBackoffCalculator(cfg BackoffConfig) *BackoffCalculator {
	return &BackoffCalculator{
		cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NewBackoffCalculatorWithSeed creates a BackoffCalculator with a specific seed.
// Useful for deterministic testing.
func NewBackoffCalculatorWithSeed(cfg BackoffConfig, seed int64) *BackoffCalculator {
	return &BackoffCalculator{
		cfg: cfg,
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Calculate returns the delay for the given attempt number (0-indexed).
func (b *BackoffCalculator) Calculate(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	delay := float64(b.cfg.InitialDelay) * math.Pow(b.cfg.Multiplier, float64(attempt))
	if delay > float64(b.cfg.MaxDelay) {
		delay = float64(b.cfg.MaxDelay)
	}

	// Add jitter: random value in range [-JitterFactor, +JitterFactor] * delay
	if b.cfg.JitterFactor > 0 {
		b.mu.Lock()
		jitter := delay * b.cfg.JitterFactor * (b.rng.Float64()*2 - 1)
		b.mu.Unlock()
		delay += jitter
	}

	// Ensure non-negative
	if delay < 0 {
		delay = 0
	}

	return time.Duration(delay)
}

// defaultBackoffCalculator is the package-level calculator for backwards compatibility.
var defaultBackoffCalculator = NewBackoffCalculator(DefaultBackoffConfig)

// CalculateBackoff returns the delay for the given attempt number (0-indexed).
// This is a convenience function that uses the default calculator.
// For testing, use BackoffCalculator with a fixed seed.
func CalculateBackoff(attempt int, cfg BackoffConfig) time.Duration {
	// For backwards compatibility, we create a temporary calculator.
	// In production, the Notifier should use its own BackoffCalculator.
	calc := &BackoffCalculator{
		cfg: cfg,
		rng: defaultBackoffCalculator.rng,
		mu:  sync.Mutex{},
	}
	return calc.Calculate(attempt)
}

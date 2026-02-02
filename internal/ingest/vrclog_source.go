package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/vrclog/vrclog-go/pkg/vrclog"
)

// Default buffer sizes for channels.
const (
	DefaultEventBufferSize = 64
	DefaultErrorBufferSize = 16
)

// VRClogSource implements EventSource using vrclog-go.
type VRClogSource struct {
	replaySince     time.Time
	logDir          string // optional override
	waitForLogs     *bool  // optional override for wait behavior (nil = default)
	logger          *slog.Logger
	eventBufferSize int
	errorBufferSize int
}

// SourceOption configures VRClogSource.
type SourceOption func(*VRClogSource)

// WithLogDir sets a custom log directory path.
// If not set, vrclog-go will auto-detect the VRChat log directory.
func WithLogDir(dir string) SourceOption {
	return func(s *VRClogSource) { s.logDir = dir }
}

// WithSourceLogger sets the logger for the source.
// If logger is nil, it is ignored and the default logger is retained.
func WithSourceLogger(logger *slog.Logger) SourceOption {
	return func(s *VRClogSource) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithWaitForLogsOption sets whether to wait for log files to appear.
// If not set (nil), the default behavior is:
//   - Wait when auto-detecting log directory (logDir is empty)
//   - Don't wait when logDir is explicitly set (fail fast on misconfiguration)
func WithWaitForLogsOption(wait bool) SourceOption {
	return func(s *VRClogSource) {
		s.waitForLogs = &wait
	}
}

// WithEventBufferSize sets the event channel buffer size.
func WithEventBufferSize(size int) SourceOption {
	return func(s *VRClogSource) { s.eventBufferSize = size }
}

// WithErrorBufferSize sets the error channel buffer size.
func WithErrorBufferSize(size int) SourceOption {
	return func(s *VRClogSource) { s.errorBufferSize = size }
}

// NewVRClogSource creates a new VRClogSource.
// replaySince specifies the time from which to replay events.
func NewVRClogSource(replaySince time.Time, opts ...SourceOption) *VRClogSource {
	s := &VRClogSource{
		replaySince:     replaySince,
		logger:          slog.Default(),
		eventBufferSize: DefaultEventBufferSize,
		errorBufferSize: DefaultErrorBufferSize,
	}
	for _, opt := range opts {
		opt(s)
	}
	// Validate buffer sizes (minimum 1 to avoid unbuffered channels)
	if s.eventBufferSize < 1 {
		s.eventBufferSize = 1
	}
	if s.errorBufferSize < 1 {
		s.errorBufferSize = 1
	}
	return s
}

// computeWaitForLogs determines whether to wait for log files to appear.
// Default behavior: wait only when auto-detecting (logDir is empty).
// When logDir is explicitly set, fail immediately to catch misconfigurations.
// This can be overridden with WithWaitForLogsOption.
func (s *VRClogSource) computeWaitForLogs() bool {
	wait := s.logDir == ""
	if s.waitForLogs != nil {
		wait = *s.waitForLogs
	}
	return wait
}

// Start begins watching VRChat logs and returns event/error channels.
// Both channels close when ctx is cancelled or on fatal error.
func (s *VRClogSource) Start(ctx context.Context) (<-chan Event, <-chan error, error) {
	// Defensive check: ensure logger is set
	if s.logger == nil {
		s.logger = slog.Default()
	}

	// Build vrclog options
	waitForLogs := s.computeWaitForLogs()
	var opts []vrclog.WatchOption
	opts = append(opts, vrclog.WithReplaySinceTime(s.replaySince))
	opts = append(opts, vrclog.WithIncludeRawLine(true))
	opts = append(opts, vrclog.WithWaitForLogs(waitForLogs))
	opts = append(opts, vrclog.WithLogger(s.logger))
	if s.logDir != "" {
		opts = append(opts, vrclog.WithLogDir(s.logDir))
	}

	s.logger.Info("starting VRChat log watcher",
		"replay_since", s.replaySince,
		"wait_for_logs", waitForLogs,
	)

	watcher, err := vrclog.NewWatcherWithOptions(opts...)
	if err != nil {
		return nil, nil, err
	}

	vrcEvents, vrcErrs, err := watcher.Watch(ctx)
	if err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}

	// Create output channels with configurable buffer sizes.
	// Buffered event channel reduces backpressure from DB latency.
	eventCh := make(chan Event, s.eventBufferSize)
	errCh := make(chan error, s.errorBufferSize)

	// Start goroutine to convert and forward events.
	// Uses nil-channel pattern: nil each channel when closed, exit when both are nil.
	logger := s.logger
	go func() {
		defer close(eventCh)
		defer close(errCh)
		defer watcher.Close()

		events := vrcEvents
		errs := vrcErrs
		var droppedErrors int64

		defer func() {
			if droppedErrors > 0 {
				logger.Warn("errors dropped due to full buffer", "count", droppedErrors)
			}
		}()

		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				select {
				case eventCh <- convertEvent(ev):
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				select {
				case errCh <- convertError(err):
				case <-ctx.Done():
					return
				default:
					droppedErrors++
				}
			}
		}
	}()

	return eventCh, errCh, nil
}

// convertEvent converts a vrclog.Event to our internal Event type.
func convertEvent(ev vrclog.Event) Event {
	return Event{
		Type:       string(ev.Type),
		Timestamp:  ev.Timestamp,
		PlayerName: ev.PlayerName,
		PlayerID:   ev.PlayerID,
		WorldID:    ev.WorldID,
		WorldName:  ev.WorldName,
		InstanceID: ev.InstanceID,
		RawLine:    ev.RawLine,
	}
}

// convertError converts vrclog errors to our internal error types.
func convertError(err error) error {
	var parseErr *vrclog.ParseError
	if errors.As(err, &parseErr) {
		return &ParseError{
			Line: parseErr.Line,
			Err:  parseErr.Err,
		}
	}
	return err
}

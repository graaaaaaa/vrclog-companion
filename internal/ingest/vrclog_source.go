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
func WithSourceLogger(logger *slog.Logger) SourceOption {
	return func(s *VRClogSource) { s.logger = logger }
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

// Start begins watching VRChat logs and returns event/error channels.
// Both channels close when ctx is cancelled or on fatal error.
func (s *VRClogSource) Start(ctx context.Context) (<-chan Event, <-chan error, error) {
	// Build vrclog options
	var opts []vrclog.WatchOption
	opts = append(opts, vrclog.WithReplaySinceTime(s.replaySince))
	opts = append(opts, vrclog.WithIncludeRawLine(true))
	if s.logDir != "" {
		opts = append(opts, vrclog.WithLogDir(s.logDir))
	}

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

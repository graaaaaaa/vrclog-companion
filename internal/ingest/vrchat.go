package ingest

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// VRChatSourceConfig configures NewVRChatSourceFactory.
type VRChatSourceConfig struct {
	// LogDir overrides the auto-detected VRChat log directory. Empty means
	// auto-detect via vrclog.DefaultLogDirectory.
	LogDir string
	// PollInterval overrides vrclog.Follow's poll interval. Zero uses
	// vrclog-go's default.
	PollInterval time.Duration
	// Logger receives the one-time cursor-missing fallback warning. Defaults
	// to slog.Default().
	Logger *slog.Logger
}

// NewVRChatSourceFactory returns a RecordSourceFactory that wraps
// vrclog.Follow for the local VRChat log directory. Every call to
// NewSource — the initial one and every rebuild after a fatal source
// error — captures a fresh LogSnapshot before Follow starts, so bytes
// accumulated during a retry gap are classified catch-up (never a live
// notification burst on recovery) and only genuinely new bytes are live.
func NewVRChatSourceFactory(cfg VRChatSourceConfig) RecordSourceFactory {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return RecordSourceFactoryFunc(func(_ context.Context, cursor *vrclog.Cursor) (RecordSource, error) {
		snapshot, err := vrclog.CaptureLogSnapshot(cfg.LogDir)
		if err != nil {
			return nil, err
		}
		return &vrChatSource{cfg: cfg, cursor: cursor, logger: logger, snapshot: snapshot}, nil
	})
}

type vrChatSource struct {
	cfg      VRChatSourceConfig
	cursor   *vrclog.Cursor
	logger   *slog.Logger
	snapshot vrclog.LogSnapshot
}

// Records implements RecordSource. If a cursor was supplied but
// vrclog.ErrCursorSourceMissing is returned before any Record is yielded,
// this logs a warning once and restarts Follow without a cursor (reading
// from the latest log file). It does not loop: a second cursor-missing
// error is treated as an ordinary fatal error and surfaced to the caller.
// The cursor-missing fallback reuses the same LogSnapshot captured at
// NewSource time — it is still the same source-retry attempt, not a new
// one.
//
// Each Record's DeliveryPhase is determined solely by s.snapshot.Contains:
// bytes that existed when the snapshot was captured are catch-up, bytes
// appended afterward (or in files created afterward) are live.
func (s *vrChatSource) Records(ctx context.Context) iter.Seq2[SourceRecord, error] {
	return func(yield func(SourceRecord, error) bool) {
		cursor := s.cursor
		fallenBack := false

		for {
			cfg := vrclog.FollowConfig{
				Directory:    s.cfg.LogDir,
				Cursor:       cursor,
				PollInterval: s.cfg.PollInterval,
			}

			missingCursor := false
			for rec, err := range vrclog.Follow(ctx, cfg) {
				if err != nil {
					if errors.Is(err, vrclog.ErrCursorSourceMissing) && !fallenBack {
						missingCursor = true
						break
					}
					if !yield(SourceRecord{}, err) {
						return
					}
					continue
				}
				phase := DeliveryLive
				if s.snapshot.Contains(rec) {
					phase = DeliveryCatchUp
				}
				if !yield(SourceRecord{Record: rec, Phase: phase}, nil) {
					return
				}
			}

			if !missingCursor {
				return
			}

			s.logger.Warn("ingest cursor source file missing, falling back to latest log without cursor")
			cursor = nil
			fallenBack = true
		}
	}
}

package app

import (
	"context"

	"github.com/vrclog/vrclog-companion/internal/projector"
)

// MediaUsecase defines the recent-media-attempts query use case.
type MediaUsecase interface {
	Recent(ctx context.Context, limit int) []*projector.MediaAttempt
}

// MediaService implements MediaUsecase by wrapping a projector.Manager.
type MediaService struct {
	Manager *projector.Manager
}

// Recent returns up to limit recent MediaAttempts, newest first.
func (s MediaService) Recent(ctx context.Context, limit int) []*projector.MediaAttempt {
	return s.Manager.RecentMedia(limit)
}

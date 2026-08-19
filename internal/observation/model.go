// Package observation defines the persistence DTO for vrclog-go
// Observations. StoredObservation is the row shape used by internal/store;
// the canonical Event lives in vrclog-go and is recovered on demand via
// DecodeEvent.
package observation

import (
	"encoding/json"
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// StoredObservation is one persisted row of the observations table.
type StoredObservation struct {
	Sequence     int64
	ID           vrclog.ObservationID
	OccurredAt   time.Time
	Type         vrclog.EventKind
	Payload      json.RawMessage
	AdapterID    vrclog.AdapterID
	RuleID       vrclog.RuleID
	RecordID     vrclog.RecordID
	SourceID     vrclog.SourceID
	SourceOffset int64
	SourceLine   uint64
	IngestedAt   time.Time
}

// DecodeEvent recovers the canonical vrclog.Event from Type + Payload.
func (o StoredObservation) DecodeEvent() (vrclog.Event, error) {
	return vrclog.DecodeEvent(o.Type, o.Payload)
}

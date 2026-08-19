package observation

import (
	"time"

	vrclog "github.com/vrclog/vrclog-go"
)

// FromVrclogObservation converts a vrclog.Observation (with its Event still
// attached) into a StoredObservation ready for persistence. ingestedAt
// should be the enclosing transaction's timestamp.
func FromVrclogObservation(obs vrclog.Observation, ingestedAt time.Time) (StoredObservation, error) {
	kind, payload, err := vrclog.EncodeEvent(obs.Event)
	if err != nil {
		return StoredObservation{}, err
	}
	return StoredObservation{
		ID:           obs.ID,
		OccurredAt:   obs.Time,
		Type:         kind,
		Payload:      payload,
		AdapterID:    obs.AdapterID,
		RuleID:       obs.RuleID,
		RecordID:     obs.Record.ID,
		SourceID:     obs.Record.SourceID,
		SourceOffset: obs.Record.Offset,
		SourceLine:   obs.Record.Line,
		IngestedAt:   ingestedAt,
	}, nil
}

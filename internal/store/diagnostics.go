package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	vrclog "github.com/vrclog/vrclog-go"
)

// maxDiagnosticMessageLen bounds the stored diagnostic message so a
// malformed/adversarial log line cannot grow the diagnostics table
// unbounded, and so redaction has a fixed budget to scan.
const maxDiagnosticMessageLen = 512

// urlPattern matches scheme://... substrings so diagnostic messages never
// carry a full URL (which may embed signed tokens or query secrets) into
// storage or, downstream, into the API response.
var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://\S+`)

// redactDiagnosticMessage strips URL-shaped substrings and caps length.
// This is intentionally applied twice: once here at write time, and again
// by the API layer before serialization, as defense in depth.
func redactDiagnosticMessage(msg string) string {
	msg = urlPattern.ReplaceAllString(msg, "<url:redacted>")
	if len(msg) <= maxDiagnosticMessageLen {
		return msg
	}
	b := []byte(msg)[:maxDiagnosticMessageLen]
	for len(b) > 0 && !utf8.RuneStart(b[len(b)-1]) {
		b = b[:len(b)-1]
	}
	return string(b) + "…(truncated)"
}

// diagnosticID deterministically derives a Diagnostic's storage identity so
// re-reading the same Record does not duplicate the same Diagnostic.
func diagnosticID(recordID vrclog.RecordID, adapterID vrclog.AdapterID, ruleID vrclog.RuleID, code vrclog.DiagnosticCode, message string) string {
	h := sha256.New()
	h.Write([]byte(recordID))
	h.Write([]byte{0})
	h.Write([]byte(adapterID))
	h.Write([]byte{0})
	h.Write([]byte(ruleID))
	h.Write([]byte{0})
	h.Write([]byte(code))
	h.Write([]byte{0})
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// CountDiagnostics returns the total number of stored Diagnostics.
func (s *Store) CountDiagnostics(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM diagnostics").Scan(&n); err != nil {
		return 0, fmt.Errorf("count diagnostics: %w", err)
	}
	return n, nil
}

func insertDiagnosticTx(ctx context.Context, tx *sql.Tx, diag vrclog.Diagnostic, now time.Time) error {
	message := redactDiagnosticMessage(diag.Message)
	id := diagnosticID(diag.Record.ID, diag.AdapterID, diag.RuleID, diag.Code, message)

	const q = `
	INSERT INTO diagnostics
		(id, record_id, source_id, source_offset, source_line, adapter_id, rule_id, code, message, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO NOTHING
	`
	_, err := tx.ExecContext(ctx, q,
		id,
		string(diag.Record.ID),
		string(diag.Record.SourceID),
		diag.Record.Offset,
		diag.Record.Line,
		string(diag.AdapterID),
		string(diag.RuleID),
		string(diag.Code),
		message,
		now.UTC().Format(TimeFormat),
	)
	if err != nil {
		return fmt.Errorf("insert diagnostic: %w", err)
	}
	return nil
}

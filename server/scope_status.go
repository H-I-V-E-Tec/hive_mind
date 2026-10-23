package server

import (
	"context"
	"encoding/json"
	"errors"
)

const maxScopeStatusBytes = (1 << 20) - 1

type ScopeStatusRule struct {
	Action    string `json:"action"`
	AssetType string `json:"asset_type"`
	Value     string `json:"value"`
}

type ScopeStatusSnapshot struct {
	SchemaVersion int               `json:"schema_version"`
	ProgramID     string            `json:"program_id"`
	Status        string            `json:"status"`
	ScopeRevision string            `json:"scope_revision"`
	Rules         []ScopeStatusRule `json:"rules"`
}

// ScopeStatus reads only the active approved scope document. It never changes
// approval state or uses an ordinary document as scope authority.
func (iw *IngestionWorker) ScopeStatus(ctx context.Context, programID string) (ScopeStatusSnapshot, error) {
	if !validIdentifier(programID, 64) {
		return ScopeStatusSnapshot{}, errors.New("invalid program_id")
	}
	result := ScopeStatusSnapshot{SchemaVersion: 1, ProgramID: programID,
		Status: "unapproved", ScopeRevision: "unapproved", Rules: []ScopeStatusRule{}}
	rows, err := iw.controlRows(ctx, "scope_approval", programID)
	if err != nil || len(rows) > 1 {
		return ScopeStatusSnapshot{}, errors.New("scope approval is unavailable")
	}
	if len(rows) == 0 {
		return result, nil
	}
	approval := rows[0].Payload
	if payloadString(approval, "program_id", "") != programID || payloadInt(approval, "schema_version") != 1 {
		return ScopeStatusSnapshot{}, errors.New("scope approval is invalid")
	}
	switch payloadString(approval, "status", "") {
	case "unapproved":
		if payloadString(approval, "scope_revision", "") != "unapproved" {
			return ScopeStatusSnapshot{}, errors.New("scope approval is invalid")
		}
		return result, nil
	case "approved":
		// Continue below after validating the exact approved revision.
	default:
		return ScopeStatusSnapshot{}, errors.New("scope approval is invalid")
	}
	revision := payloadString(approval, "scope_revision", "")
	contentHash := payloadString(approval, "scope_document_hash", "")
	if !validScopeApprovalHash(revision) || !validScopeApprovalHash(contentHash) {
		return ScopeStatusSnapshot{}, errors.New("scope approval is invalid")
	}
	content, err := iw.approvedScopeContent(ctx, programID, revision)
	if err != nil || sha256Hex([]byte(content)) != contentHash {
		return ScopeStatusSnapshot{}, errors.New("approved scope document is unavailable")
	}
	manifest, err := parseScopeManifest([]byte(content), programID)
	if err != nil || manifest == nil || len(manifest.Rules) == 0 || len(manifest.Rules) > 10000 {
		return ScopeStatusSnapshot{}, errors.New("approved scope document is unavailable")
	}
	result.Status, result.ScopeRevision = "approved", revision
	includes := 0
	for _, rule := range manifest.Rules {
		if rule.Action == "include" {
			includes++
		}
		result.Rules = append(result.Rules, ScopeStatusRule{
			Action: rule.Action, AssetType: rule.AssetType, Value: rule.normalized.Value,
		})
	}
	if includes == 0 {
		return ScopeStatusSnapshot{}, errors.New("approved scope has no inclusion")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxScopeStatusBytes {
		return ScopeStatusSnapshot{}, errors.New("approved scope status exceeds output limit")
	}
	latest, err := iw.controlRows(ctx, "scope_approval", programID)
	if err != nil || len(latest) != 1 || payloadString(latest[0].Payload, "status", "") != "approved" ||
		payloadString(latest[0].Payload, "scope_revision", "") != revision ||
		payloadString(latest[0].Payload, "scope_document_hash", "") != contentHash ||
		payloadInt(latest[0].Payload, "control_version") != payloadInt(approval, "control_version") {
		return ScopeStatusSnapshot{}, errors.New("scope approval changed during read")
	}
	return result, nil
}

func validScopeStatusSnapshot(snapshot ScopeStatusSnapshot, programID string) bool {
	if snapshot.SchemaVersion != 1 || snapshot.ProgramID != programID || snapshot.Rules == nil || len(snapshot.Rules) > 10000 {
		return false
	}
	if snapshot.Status == "unapproved" {
		return snapshot.ScopeRevision == "unapproved" && len(snapshot.Rules) == 0
	}
	if snapshot.Status != "approved" || !validScopeApprovalHash(snapshot.ScopeRevision) || len(snapshot.Rules) == 0 {
		return false
	}
	includes := 0
	seen := make(map[string]bool, len(snapshot.Rules))
	for _, rule := range snapshot.Rules {
		if rule.Action != "include" && rule.Action != "exclude" || len(rule.Value) == 0 || len(rule.Value) > 2048 {
			return false
		}
		normalized, err := normalizeTypedAsset(rule.AssetType, rule.Value)
		if err != nil || normalized.Value != rule.Value {
			return false
		}
		key := rule.Action + "\x00" + rule.AssetType + "\x00" + rule.Value
		if seen[key] {
			return false
		}
		seen[key] = true
		if rule.Action == "include" {
			includes++
		}
	}
	return includes > 0
}

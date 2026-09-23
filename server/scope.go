package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"golang.org/x/net/idna"
)

type normalizedAsset struct {
	Type  string
	Value string
}

type scopeRule struct {
	Action     string `json:"action"`
	AssetType  string `json:"asset_type"`
	Value      string `json:"value"`
	Reason     string `json:"reason,omitempty"`
	normalized normalizedAsset
}

type scopeManifest struct {
	SchemaVersion  int         `json:"schema_version"`
	ProgramID      string      `json:"program_id"`
	Platform       string      `json:"platform,omitempty"`
	TargetName     string      `json:"target_name,omitempty"`
	Classification string      `json:"classification,omitempty"`
	Source         string      `json:"source"`
	CollectedAt    string      `json:"collected_at"`
	Rules          []scopeRule `json:"rules"`
}

type ScopeApprovalSummary struct {
	ProgramID string `json:"program_id"`
	SHA256    string `json:"sha256"`
	Rules     int    `json:"rules"`
	Includes  int    `json:"includes"`
	Excludes  int    `json:"excludes"`
}

// ScopeApprovalPreview validates the canonical file without changing Qdrant
// and returns only non-sensitive counts and the exact byte hash to approve.
func (iw *IngestionWorker) ScopeApprovalPreview(programID string) (ScopeApprovalSummary, error) {
	if !iw.Cfg.IsWriter() {
		return ScopeApprovalSummary{}, errors.New("scope approval requires HIVE_ROLE=writer")
	}
	if !validIdentifier(programID, 64) {
		return ScopeApprovalSummary{}, errors.New("invalid program_id")
	}
	content, manifest, err := iw.loadScopeManifest(programID)
	if err != nil {
		return ScopeApprovalSummary{}, err
	}
	summary := ScopeApprovalSummary{ProgramID: programID, SHA256: sha256Hex(content), Rules: len(manifest.Rules)}
	for _, rule := range manifest.Rules {
		if rule.Action == "include" {
			summary.Includes++
		} else {
			summary.Excludes++
		}
	}
	return summary, nil
}

func parseScopeManifest(content []byte, pathProgramID string) (*scopeManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest scopeManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid scope manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid scope manifest: trailing JSON value")
	}
	if manifest.SchemaVersion != 1 || manifest.ProgramID != pathProgramID || !validIdentifier(manifest.ProgramID, 64) {
		return nil, errors.New("scope manifest schema_version or program_id is invalid")
	}
	if (manifest.Platform == "") != (manifest.TargetName == "") ||
		(manifest.Platform != "" && (!validIdentifier(manifest.Platform, 64) || manifest.TargetName != "@program")) {
		return nil, errors.New("scope manifest platform and target_name must identify @program together")
	}
	if manifest.Classification != "" && manifest.Classification != "internal" && manifest.Classification != "restricted" {
		return nil, errors.New("scope manifest classification is invalid")
	}
	if strings.TrimSpace(manifest.Source) == "" || len(manifest.Source) > 500 {
		return nil, errors.New("scope manifest source is invalid")
	}
	collected, err := time.Parse(time.RFC3339, manifest.CollectedAt)
	if err != nil {
		return nil, errors.New("scope manifest collected_at is not RFC 3339")
	}
	manifest.CollectedAt = collected.UTC().Format(time.RFC3339)
	if len(manifest.Rules) == 0 || len(manifest.Rules) > 10000 {
		return nil, errors.New("scope manifest must contain between 1 and 10000 rules")
	}
	seen := make(map[string]bool)
	for i := range manifest.Rules {
		rule := &manifest.Rules[i]
		if rule.Action != "include" && rule.Action != "exclude" {
			return nil, fmt.Errorf("scope rule %d has invalid action", i)
		}
		if len(rule.Reason) > 500 || len(rule.Value) == 0 || len(rule.Value) > 2048 {
			return nil, fmt.Errorf("scope rule %d has invalid value or reason", i)
		}
		normalized, err := normalizeTypedAsset(rule.AssetType, rule.Value)
		if err != nil {
			return nil, fmt.Errorf("scope rule %d: %w", i, err)
		}
		rule.normalized = normalized
		key := rule.Action + "\x00" + normalized.Type + "\x00" + normalized.Value
		if seen[key] {
			return nil, fmt.Errorf("scope rule %d duplicates a normalized rule", i)
		}
		seen[key] = true
	}
	return &manifest, nil
}

func normalizeAssetReference(value string) (normalizedAsset, error) {
	value = strings.TrimSpace(value)
	for _, kind := range []string{"host", "wildcard_domain", "ip", "cidr", "url_prefix"} {
		if strings.HasPrefix(value, kind+":") {
			return normalizeTypedAsset(kind, strings.TrimSpace(strings.TrimPrefix(value, kind+":")))
		}
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return normalizeTypedAsset("url_prefix", value)
	}
	if strings.HasPrefix(value, "*.") {
		return normalizeTypedAsset("wildcard_domain", value)
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return normalizedAsset{Type: "cidr", Value: prefix.Masked().String()}, nil
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return normalizedAsset{Type: "ip", Value: address.String()}, nil
	}
	return normalizeTypedAsset("host", value)
}

func normalizeTypedAsset(kind, value string) (normalizedAsset, error) {
	value = strings.TrimSpace(value)
	switch kind {
	case "host":
		host, err := normalizeHost(value)
		return normalizedAsset{Type: kind, Value: host}, err
	case "wildcard_domain":
		if !strings.HasPrefix(value, "*.") {
			return normalizedAsset{}, errors.New("wildcard_domain must start with *.")
		}
		host, err := normalizeHost(strings.TrimPrefix(value, "*."))
		return normalizedAsset{Type: kind, Value: "*." + host}, err
	case "ip":
		address, err := netip.ParseAddr(value)
		if err != nil {
			return normalizedAsset{}, errors.New("invalid IP address")
		}
		return normalizedAsset{Type: kind, Value: address.String()}, nil
	case "cidr":
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix != prefix.Masked() {
			return normalizedAsset{}, errors.New("CIDR must be a canonical network")
		}
		return normalizedAsset{Type: kind, Value: prefix.String()}, nil
	case "url_prefix":
		normalized, err := normalizeURLPrefix(value)
		return normalizedAsset{Type: kind, Value: normalized}, err
	default:
		return normalizedAsset{}, fmt.Errorf("unsupported asset_type %q", kind)
	}
}

func normalizeHost(value string) (string, error) {
	if value == "" || strings.HasSuffix(value, ".") || strings.ContainsAny(value, "/:@[] ") {
		return "", errors.New("invalid host")
	}
	host, err := idna.Lookup.ToASCII(strings.ToLower(value))
	if err != nil || host == "" || len(host) > 253 {
		return "", errors.New("invalid host")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("invalid host")
		}
	}
	return host, nil
}

func normalizeURLPrefix(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return "", errors.New("url_prefix must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	hostname := u.Hostname()
	if address, ipErr := netip.ParseAddr(hostname); ipErr == nil {
		hostname = address.String()
		if address.Is6() {
			hostname = "[" + hostname + "]"
		}
	} else {
		hostname, err = normalizeHost(hostname)
		if err != nil {
			return "", err
		}
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		number, parseErr := strconv.Atoi(port)
		if parseErr != nil || number < 1 || number > 65535 {
			return "", errors.New("invalid URL port")
		}
		hostname = net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}
	u.Host = hostname
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func effectiveScopeStatus(manifest *scopeManifest, assets []normalizedAsset) string {
	if manifest == nil || len(assets) == 0 {
		return "unknown"
	}
	allIncluded := true
	for _, asset := range assets {
		included := false
		for _, rule := range manifest.Rules {
			if ruleMatchesAsset(rule.normalized, asset) {
				if rule.Action == "exclude" {
					return "out_of_scope"
				}
				included = true
			}
		}
		if !included {
			allIncluded = false
		}
	}
	if allIncluded {
		return "authorized"
	}
	return "unknown"
}

func ruleMatchesAsset(rule, asset normalizedAsset) bool {
	assetHost := ""
	if asset.Type == "host" || asset.Type == "ip" {
		assetHost = asset.Value
	} else if asset.Type == "url_prefix" {
		u, _ := url.Parse(asset.Value)
		assetHost = u.Hostname()
	}
	switch rule.Type {
	case "host":
		return assetHost == rule.Value
	case "wildcard_domain":
		root := strings.TrimPrefix(rule.Value, "*.")
		return assetHost != root && strings.HasSuffix(assetHost, "."+root)
	case "ip":
		return assetHost == rule.Value
	case "cidr":
		prefix, _ := netip.ParsePrefix(rule.Value)
		address, err := netip.ParseAddr(assetHost)
		return err == nil && prefix.Contains(address)
	case "url_prefix":
		if asset.Type != "url_prefix" {
			return false
		}
		ruleURL, _ := url.Parse(rule.Value)
		assetURL, _ := url.Parse(asset.Value)
		if ruleURL.Scheme != assetURL.Scheme || ruleURL.Host != assetURL.Host {
			return false
		}
		rulePath := strings.TrimSuffix(ruleURL.EscapedPath(), "/")
		assetPath := strings.TrimSuffix(assetURL.EscapedPath(), "/")
		return assetPath == rulePath || strings.HasPrefix(assetPath, rulePath+"/")
	}
	return false
}

func (iw *IngestionWorker) loadScopeManifest(programID string) ([]byte, *scopeManifest, error) {
	path := filepath.Join(iw.Cfg.DataDirectory, "programs", programID, "scope.json")
	content, _, pathProgram, _, err := iw.secureReadDocument(path)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := parseScopeManifest(content, pathProgram)
	return content, manifest, err
}

// resolveActiveScope fails closed and immediately invalidates an approval when
// the approving writer's canonical manifest no longer has the approved byte
// hash. Other writers never judge their local copy: they use the approved
// revision and reconstruct the manifest from the published scope document, so a
// device without the file cannot revoke an approval it does not own.
func (iw *IngestionWorker) resolveActiveScope(ctx context.Context, programID string) (string, *scopeManifest, error) {
	rows, err := iw.controlRows(ctx, "scope_approval", programID)
	if err != nil {
		return "", nil, err
	}
	if len(rows) == 0 {
		if iw.Cfg.IsWriter() {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if err := iw.upsertControl(ctx, "scope_approval", programID, map[string]any{
				"program_id": programID, "status": "unapproved", "scope_revision": "unapproved", "schema_version": int64(1), "updated_at": now,
			}); err != nil {
				return "", nil, err
			}
		}
		return "unapproved", nil, nil
	}
	if len(rows) != 1 || payloadString(rows[0].Payload, "status", "") != "approved" {
		return "unapproved", nil, nil
	}
	revision := payloadString(rows[0].Payload, "scope_revision", "")
	if len(revision) != 64 {
		return "", nil, errors.New("invalid approved scope revision")
	}
	if !iw.Cfg.IsWriter() {
		return revision, nil, nil
	}
	if approver := payloadString(rows[0].Payload, "approved_by_device_id", ""); approver != "" && approver != iw.Cfg.DeviceID {
		manifest, err := iw.reconstructApprovedScope(ctx, programID, revision)
		if err != nil {
			return revision, nil, nil
		}
		return revision, manifest, nil
	}
	content, manifest, manifestErr := iw.loadScopeManifest(programID)
	if manifestErr == nil && sha256Hex(content) == revision {
		return revision, manifest, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := iw.upsertControl(ctx, "scope_approval", programID, map[string]any{
		"program_id": programID, "status": "unapproved", "scope_revision": "unapproved", "schema_version": int64(1), "updated_at": now,
	}); err != nil {
		return "", nil, fmt.Errorf("invalidate changed scope approval: %w", err)
	}
	return "unapproved", nil, nil
}

func (iw *IngestionWorker) controlScopeRevision(ctx context.Context, programID string) (string, error) {
	rows, err := iw.controlRows(ctx, "scope_approval", programID)
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", errors.New("exactly one scope approval record is required")
	}
	if payloadString(rows[0].Payload, "status", "") != "approved" {
		return "unapproved", nil
	}
	revision := payloadString(rows[0].Payload, "scope_revision", "")
	if len(revision) != 64 {
		return "", errors.New("invalid approved scope revision")
	}
	return revision, nil
}

// ApproveScope validates the canonical manifest. Full no-reembedding
// rematerialization is delegated to rematerializeProgramScope below.
func (iw *IngestionWorker) ApproveScope(ctx context.Context, programID string) error {
	return iw.ApproveScopeRevision(ctx, programID, "")
}

func (iw *IngestionWorker) ApproveScopeRevision(ctx context.Context, programID, confirmedHash string) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("scope approval requires HIVE_ROLE=writer")
	}
	if !validIdentifier(programID, 64) {
		return errors.New("invalid program_id")
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return err
	}
	content, manifest, err := iw.loadScopeManifest(programID)
	if err != nil {
		return err
	}
	revision := sha256Hex(content)
	if confirmedHash != "" && revision != confirmedHash {
		return &operationalError{ExitUsage, "scope manifest changed since confirmation"}
	}
	if err := iw.audit(AuditEvent{Action: "scope_approval", Outcome: "prepared", Program: programID, Revision: revision}, true); err != nil {
		return err
	}
	staged, err := iw.rematerializeProgramScope(ctx, programID, revision, manifest)
	if err != nil {
		return err
	}
	current, _, err := iw.loadScopeManifest(programID)
	if err != nil || sha256Hex(current) != revision {
		return &operationalError{ExitPartialFailure, "scope manifest changed during staging"}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	committedHeads := make([]*documentHead, 0, len(staged))
	for _, item := range staged {
		if err := iw.upsertControl(ctx, "document_head", item.head.DocumentID, scopeHeadPayload(item.head, revision, now)); err != nil {
			for _, old := range committedHeads {
				_ = iw.upsertControl(ctx, "document_head", old.DocumentID, scopeHeadPayload(old, old.ScopeRevision, now))
			}
			return fmt.Errorf("commit rematerialized document %s: %w", filepath.Base(item.head.Path), err)
		}
		committedHeads = append(committedHeads, item.head)
	}
	if err := iw.upsertControl(ctx, "scope_approval", programID, map[string]any{
		"program_id": programID, "status": "approved", "scope_revision": revision, "schema_version": int64(1),
		"approved_by_device_id": iw.Cfg.DeviceID, "approved_at": now, "updated_at": now,
	}); err != nil {
		for _, old := range committedHeads {
			_ = iw.upsertControl(ctx, "document_head", old.DocumentID, scopeHeadPayload(old, old.ScopeRevision, now))
		}
		return fmt.Errorf("activate scope approval: %w", err)
	}
	for _, item := range staged {
		if item.head.ScopeRevision != revision {
			if _, err := iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(revisionFilter(item.head.DocumentID, item.head.DocumentRevision, item.head.ScopeRevision))}); err != nil {
				log.Printf("scope approval active; stale revision cleanup pending for document_id=%s: %v", item.head.DocumentID, err)
			}
		}
	}
	return nil
}

type stagedScopeDocument struct{ head *documentHead }

// scopeHeadPayload rewrites a head under a new scope revision while keeping
// its owner, so approving scope never transfers documents between writers.
func scopeHeadPayload(head *documentHead, revision, now string) map[string]any {
	rescoped := *head
	rescoped.ScopeRevision = revision
	return headPayload(&rescoped, now)
}

func (iw *IngestionWorker) rematerializeProgramScope(ctx context.Context, programID, revision string, manifest *scopeManifest) ([]stagedScopeDocument, error) {
	headFilter := &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "document_head"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID), qdrant.NewMatchKeyword("program_id", programID),
	}}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{CollectionName: iw.Cfg.ControlCollection, Filter: headFilter,
		Limit: qdrant.PtrOf(uint32(5000)), WithPayload: qdrant.NewWithPayloadEnable(true)})
	if err != nil {
		return nil, err
	}
	headCount, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.ControlCollection, Exact: qdrant.PtrOf(true), Filter: headFilter})
	if err != nil || headCount != uint64(len(rows)) {
		return nil, errors.New("program has too many or incomplete document heads to approve safely")
	}
	sort.Slice(rows, func(i, j int) bool {
		return payloadString(rows[i].Payload, "path", "") < payloadString(rows[j].Payload, "path", "")
	})
	staged := make([]stagedScopeDocument, 0, len(rows))
	for _, row := range rows {
		head, err := iw.readDocumentHead(ctx, payloadString(row.Payload, "document_id", ""))
		if err != nil || head == nil {
			return nil, errors.New("invalid document head during scope rematerialization")
		}
		if head.State == "deleted" {
			continue
		}
		points, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{CollectionName: iw.Cfg.CollectionName, Filter: revisionFilter(head.DocumentID, head.DocumentRevision, head.ScopeRevision),
			Limit: qdrant.PtrOf(uint32(head.ChunkCount + 1)), WithPayload: qdrant.NewWithPayloadEnable(true), WithVectors: qdrant.NewWithVectorsEnable(true)})
		if err != nil || len(points) != head.ChunkCount {
			return nil, fmt.Errorf("active document %s is incomplete", filepath.Base(head.Path))
		}
		newPoints := make([]*qdrant.PointStruct, 0, len(points))
		for _, point := range points {
			ordinal := payloadInt(point.Payload, "chunk_ordinal")
			payload := clonePayload(point.Payload)
			assets, err := normalizedAssetsFromPayload(payload["asset_refs"])
			if err != nil {
				return nil, err
			}
			payload["scope_revision"] = qdrant.NewValueString(revision)
			payload["effective_scope_status"] = qdrant.NewValueString(effectiveScopeStatus(manifest, assets))
			vectors, err := vectorsOutputToInput(point.Vectors)
			if err != nil {
				return nil, fmt.Errorf("document %s has no reusable vectors", filepath.Base(head.Path))
			}
			newPoints = append(newPoints, &qdrant.PointStruct{Id: qdrant.NewIDUUID(deterministicUUID("chunk", head.DocumentID, head.DocumentRevision, revision, fmt.Sprint(ordinal))), Vectors: vectors, Payload: payload})
		}
		if _, err := iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: newPoints}); err != nil {
			return nil, err
		}
		count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.CollectionName, Exact: qdrant.PtrOf(true), Filter: revisionFilter(head.DocumentID, head.DocumentRevision, revision)})
		if err != nil || count != uint64(head.ChunkCount) {
			return nil, fmt.Errorf("scope rematerialization verification failed for %s", filepath.Base(head.Path))
		}
		staged = append(staged, stagedScopeDocument{head: head})
	}
	return staged, nil
}

func clonePayload(payload map[string]*qdrant.Value) map[string]*qdrant.Value {
	out := make(map[string]*qdrant.Value, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func normalizedAssetsFromPayload(value *qdrant.Value) ([]normalizedAsset, error) {
	if value == nil || value.GetListValue() == nil {
		return nil, nil
	}
	var assets []normalizedAsset
	for _, item := range value.GetListValue().Values {
		asset, err := normalizeAssetReference(item.GetStringValue())
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func vectorsOutputToInput(output *qdrant.VectorsOutput) (*qdrant.Vectors, error) {
	if output == nil {
		return nil, errors.New("missing vectors")
	}
	if vector := output.GetVector(); vector != nil {
		if dense := vector.GetDense(); dense != nil {
			return qdrant.NewVectorsDense(dense.GetData()), nil
		}
		if sparse := vector.GetSparse(); sparse != nil {
			return qdrant.NewVectorsSparse(sparse.Indices, sparse.Values), nil
		}
		return nil, errors.New("unsupported stored vector")
	}
	named := output.GetVectors()
	if named == nil {
		return nil, errors.New("missing vectors")
	}
	vectors := make(map[string]*qdrant.Vector, len(named.Vectors))
	for name, outputVector := range named.Vectors {
		if dense := outputVector.GetDense(); dense != nil {
			vectors[name] = qdrant.NewVector(dense.Data...)
		} else if sparse := outputVector.GetSparse(); sparse != nil {
			vectors[name] = qdrant.NewVectorSparse(sparse.Indices, sparse.Values)
		} else {
			return nil, errors.New("unsupported stored vector")
		}
	}
	return qdrant.NewVectorsMap(vectors), nil
}

// Retained for call sites implemented by spec 02.
func (iw *IngestionWorker) activeScopeRevision(ctx context.Context, programID string) (string, error) {
	revision, _, err := iw.resolveActiveScope(ctx, programID)
	return revision, err
}

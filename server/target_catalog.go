package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

const (
	maxCatalogScanDocuments     = 5000 // only ordinal-zero chunks are scanned
	maxCatalogAssetsPerDocument = 100
	maxCatalogAssetsPerTarget   = 500
	maxCatalogResponseBytes     = 60 * 1024
	maxCatalogPrograms          = 100
)

type HiveListTargetsArguments struct {
	ProgramID          string `json:"program_id"`
	Limit              *int   `json:"limit,omitempty"`
	Order              string `json:"order,omitempty"`
	IncludeUnconfirmed bool   `json:"include_unconfirmed,omitempty"`
}

type TargetCoverage struct {
	ReconDocuments    int `json:"recon_documents"`
	NoteDocuments     int `json:"note_documents"`
	EvidenceDocuments int `json:"evidence_documents"`
	DistinctSources   int `json:"distinct_sources"`
}

type CatalogAsset struct {
	Type          string `json:"type"`
	Value         string `json:"value"`
	ScopeStatus   string `json:"scope_status"`
	ActionAllowed bool   `json:"action_allowed"`
}

type TargetScopeSummary struct {
	Status           string `json:"status"`
	AuthorizedAssets int    `json:"authorized_assets"`
	ExcludedAssets   int    `json:"excluded_assets"`
	UnknownAssets    int    `json:"unknown_assets"`
	ScopeRevision    string `json:"scope_revision"`
	ActionAllowed    bool   `json:"action_allowed"` // A project name is never permission to scan.
}

type TargetCatalogEntry struct {
	ProgramID      string             `json:"program_id"`
	Platform       string             `json:"platform"`
	TargetName     string             `json:"target_name"`
	Scope          TargetScopeSummary `json:"scope"`
	ObservedAssets []CatalogAsset     `json:"observed_assets"`
	Rank           int                `json:"rank,omitempty"`
	Band           string             `json:"band,omitempty"`
	Reasons        []string           `json:"reasons"`
	Coverage       TargetCoverage     `json:"coverage"`
	LatestAt       string             `json:"latest_at,omitempty"`
	SourcePaths    []string           `json:"source_paths"`
}

type HiveListTargetsResponse struct {
	ProgramID           string               `json:"program_id"`
	ScopeRevision       string               `json:"scope_revision"`
	Targets             []TargetCatalogEntry `json:"targets"`
	Unconfirmed         []TargetCatalogEntry `json:"unconfirmed"`
	CandidatesEvaluated int                  `json:"candidates_evaluated"`
	UnregisteredFiles   int                  `json:"unregistered_files"`
	Warnings            []string             `json:"warnings"`
	Truncated           bool                 `json:"truncated"`
}

type catalogDocument struct {
	kind, path, source, latest string
	assets                     []normalizedAsset
}

type catalogCandidate struct {
	entry     TargetCatalogEntry
	documents map[string]catalogDocument
	assets    map[string]normalizedAsset
}

func validateHiveListTargets(args HiveListTargetsArguments) (HiveListTargetsArguments, int, error) {
	if args.ProgramID != "" && !validIdentifier(args.ProgramID, 64) {
		return args, 0, invalidSearch("program_id is invalid")
	}
	limit := 20
	if args.ProgramID == "" {
		limit = 10
	}
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit < 1 || limit > 50 {
		return args, 0, invalidSearch("limit must be between 1 and 50")
	}
	if args.Order == "" {
		args.Order = "balanced"
	}
	if args.Order != "balanced" && args.Order != "most_documented" && args.Order != "needs_recon" {
		return args, 0, invalidSearch("order is invalid")
	}
	return args, limit, nil
}

// This is an inventory of named projects, not authorization to test them.
// Only individual concrete assets can be confirmed by approved scope rules.
func (iw *IngestionWorker) HiveListTargets(ctx context.Context, args HiveListTargetsArguments) (out HiveListTargetsResponse, resultErr error) {
	started := time.Now()
	defer func() {
		outcome := "completed"
		if resultErr != nil {
			outcome = "failed"
		}
		encoded, _ := json.Marshal(out)
		_ = iw.audit(AuditEvent{Action: "hive_list_targets", Outcome: outcome, Program: args.ProgramID, Count: len(out.Targets),
			DurationMS: time.Since(started).Milliseconds(), ResponseChars: int64(len(encoded)), Truncated: out.Truncated}, false)
	}()
	args, limit, err := validateHiveListTargets(args)
	if err != nil {
		return out, err
	}
	if args.ProgramID == "" {
		return iw.listTargetsAcrossPrograms(ctx, args, limit)
	}
	return iw.listTargetsInProgram(ctx, args, limit)
}

// A zero limit keeps all candidates for the final cross-program ranking.
func (iw *IngestionWorker) listTargetsInProgram(ctx context.Context, args HiveListTargetsArguments, limit int) (out HiveListTargetsResponse, resultErr error) {
	revision, manifest, warning, err := iw.contextScopeManifest(ctx, args.ProgramID)
	if err != nil {
		return out, errors.New("approved scope could not be verified")
	}
	out = HiveListTargetsResponse{ProgramID: args.ProgramID, ScopeRevision: revision,
		Targets: []TargetCatalogEntry{}, Unconfirmed: []TargetCatalogEntry{}, Warnings: []string{}}
	if warning != "" {
		out.Warnings = append(out.Warnings, warning)
	}
	filter := &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "chunk"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
		qdrant.NewMatchKeyword("program_id", args.ProgramID), qdrant.NewMatchKeyword("scope_revision", revision),
		qdrant.NewMatchInt("chunk_ordinal", 0),
	}}
	if iw.Cfg.MaxClassification == "restricted" {
		filter.Must = append(filter.Must, qdrant.NewMatchKeywords("classification", "internal", "restricted"))
	} else {
		filter.Must = append(filter.Must, qdrant.NewMatchKeyword("classification", "internal"))
	}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{CollectionName: iw.Cfg.CollectionName, Filter: filter,
		Limit: qdrant.PtrOf(uint32(maxCatalogScanDocuments)), WithPayload: qdrant.NewWithPayloadInclude(
			"hive_id", "program_id", "record_type", "scope_revision", "classification", "document_id", "path",
			"document_revision", "chunk_ordinal", "document_type", "platform", "target_name", "observed_targets",
			"asset_refs", "source", "collected_at", "indexed_at")})
	if err != nil {
		return out, errors.New("target catalog index is unavailable")
	}
	count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.CollectionName, Exact: qdrant.PtrOf(true), Filter: filter})
	if err != nil {
		return out, errors.New("target catalog count is unavailable")
	}
	if len(rows) > maxCatalogScanDocuments {
		rows = rows[:maxCatalogScanDocuments]
	}
	if count > uint64(len(rows)) {
		out.Truncated = true
		out.Warnings = appendWarning(out.Warnings, "catalog scan limit reached; coverage is partial")
	}
	candidates := map[string]*catalogCandidate{}
	seenDocuments := map[string]bool{}
	for _, row := range rows {
		payload := row.Payload
		if payloadString(payload, "hive_id", "") != iw.Cfg.HiveID || payloadString(payload, "program_id", "") != args.ProgramID ||
			payloadString(payload, "record_type", "") != "chunk" || payloadString(payload, "scope_revision", "") != revision ||
			payloadInt(payload, "chunk_ordinal") != 0 {
			continue
		}
		classification := payloadString(payload, "classification", "unknown")
		if classification != "internal" && !(classification == "restricted" && iw.Cfg.MaxClassification == "restricted") {
			continue
		}
		docID := payloadString(payload, "document_id", "")
		if docID == "" || seenDocuments[docID] {
			continue
		}
		path := filepath.ToSlash(payloadString(payload, "path", ""))
		if filepath.IsAbs(path) || strings.Contains(path, "\\") || path != filepath.ToSlash(filepath.Clean(path)) ||
			!strings.HasPrefix(path, "programs/"+args.ProgramID+"/") {
			continue
		}
		head, err := iw.readDocumentHead(ctx, docID)
		if err != nil {
			return out, errors.New("target catalog head validation failed")
		}
		if head == nil || head.State != "active" || head.ProgramID != args.ProgramID || head.Path != path ||
			head.DocumentRevision != payloadString(payload, "document_revision", "") || head.ScopeRevision != revision {
			continue
		}
		tombstoned, err := iw.isTombstoned(ctx, docID)
		if err != nil {
			return out, errors.New("target catalog tombstone validation failed")
		}
		if tombstoned {
			continue
		}
		seenDocuments[docID] = true
		kind := payloadString(payload, "document_type", "unknown")
		if kind == "scope" || kind == "rules" {
			continue
		}
		platform, targetName := payloadString(payload, "platform", ""), payloadString(payload, "target_name", "")
		if !validIdentifier(platform, 64) || !validTargetName(targetName, kind) {
			out.UnregisteredFiles++
			continue
		}
		key := platform + "\x00" + strings.ToLower(strings.Join(strings.Fields(targetName), " "))
		candidate := candidates[key]
		if candidate == nil {
			candidate = &catalogCandidate{entry: TargetCatalogEntry{ProgramID: args.ProgramID, Platform: platform, TargetName: targetName,
				ObservedAssets: []CatalogAsset{}, Reasons: []string{}, SourcePaths: []string{}},
				documents: map[string]catalogDocument{}, assets: map[string]normalizedAsset{}}
			candidates[key] = candidate
		} else if targetName < candidate.entry.TargetName {
			candidate.entry.TargetName = targetName
		}
		assets, clipped := catalogAssetsFromPayload(payload)
		if clipped {
			out.Truncated = true
			out.Warnings = appendWarning(out.Warnings, "observed asset limit reached; coverage is partial")
		}
		boundedAssets := make([]normalizedAsset, 0, len(assets))
		for _, asset := range assets {
			key := asset.Type + "\x00" + asset.Value
			if len(candidate.assets) >= maxCatalogAssetsPerTarget && candidate.assets[key] == (normalizedAsset{}) {
				out.Truncated = true
				out.Warnings = appendWarning(out.Warnings, "target asset limit reached; coverage is partial")
				continue
			}
			candidate.assets[key] = asset
			boundedAssets = append(boundedAssets, asset)
		}
		latest := payloadString(payload, "collected_at", "")
		if latest == "" {
			latest = payloadString(payload, "indexed_at", "")
		}
		candidate.documents[docID] = catalogDocument{kind: kind, path: path,
			source: payloadString(payload, "source", ""), latest: latest, assets: boundedAssets}
	}
	if out.UnregisteredFiles > 0 {
		out.Warnings = appendWarning(out.Warnings, "some visible active documents lack reviewed platform/target registration; coverage is partial")
	}
	out.CandidatesEvaluated = len(candidates)
	authorized, unconfirmed := []*catalogCandidate{}, []*catalogCandidate{}
	for _, candidate := range candidates {
		completeCatalogCandidate(candidate, manifest, revision)
		if len(candidate.assets) > 20 {
			out.Truncated = true
			out.Warnings = appendWarning(out.Warnings, "only the first 20 observed assets per target are shown")
		}
		if candidate.entry.Scope.AuthorizedAssets > 0 {
			authorized = append(authorized, candidate)
		} else if args.IncludeUnconfirmed {
			unconfirmed = append(unconfirmed, candidate)
		}
	}
	orderCatalogCandidates(authorized, args.Order)
	if limit > 0 && len(authorized) > limit {
		out.Truncated = true
		authorized = authorized[:limit]
	}
	for rank, candidate := range authorized {
		candidate.entry.Rank = rank + 1
		out.Targets = append(out.Targets, candidate.entry)
	}
	if args.IncludeUnconfirmed {
		sort.Slice(unconfirmed, func(i, j int) bool { return catalogKey(unconfirmed[i]) < catalogKey(unconfirmed[j]) })
		if limit > 0 && len(unconfirmed) > limit {
			out.Truncated = true
			unconfirmed = unconfirmed[:limit]
		}
		for _, candidate := range unconfirmed {
			out.Unconfirmed = append(out.Unconfirmed, candidate.entry)
		}
	}
	if limit == 0 {
		return out, nil
	}
	return fitCatalogResponse(out), nil
}

func (iw *IngestionWorker) listTargetsAcrossPrograms(ctx context.Context, args HiveListTargetsArguments, limit int) (HiveListTargetsResponse, error) {
	out := HiveListTargetsResponse{Targets: []TargetCatalogEntry{}, Unconfirmed: []TargetCatalogEntry{}, Warnings: []string{}}
	filter := &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "scope_approval"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
		qdrant.NewMatchKeyword("status", "approved"),
	}}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{CollectionName: iw.Cfg.ControlCollection, Filter: filter,
		Limit: qdrant.PtrOf(uint32(maxCatalogPrograms)), WithPayload: qdrant.NewWithPayloadInclude("hive_id", "record_type", "status", "program_id")})
	if err != nil {
		return out, errors.New("approved program index is unavailable")
	}
	count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.ControlCollection, Exact: qdrant.PtrOf(true), Filter: filter})
	if err != nil {
		return out, errors.New("approved program count is unavailable")
	}
	if len(rows) > maxCatalogPrograms {
		rows = rows[:maxCatalogPrograms]
	}
	if count > uint64(len(rows)) {
		out.Truncated = true
		out.Warnings = appendWarning(out.Warnings, "program discovery limit reached; ranking is partial")
	}
	programs := map[string]bool{}
	for _, row := range rows {
		payload := row.Payload
		if payloadString(payload, "hive_id", "") != iw.Cfg.HiveID || payloadString(payload, "record_type", "") != "scope_approval" ||
			payloadString(payload, "status", "") != "approved" {
			continue
		}
		id := payloadString(payload, "program_id", "")
		if validIdentifier(id, 64) {
			programs[id] = true
		}
	}
	ids := make([]string, 0, len(programs))
	for id := range programs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	authorized, unconfirmed := []*catalogCandidate{}, []*catalogCandidate{}
	for _, id := range ids {
		programArgs := args
		programArgs.ProgramID = id
		program, err := iw.listTargetsInProgram(ctx, programArgs, 0)
		if err != nil {
			return out, errors.New("target catalog could not verify all approved programs")
		}
		out.CandidatesEvaluated += program.CandidatesEvaluated
		out.UnregisteredFiles += program.UnregisteredFiles
		if program.Truncated {
			out.Truncated = true
			out.Warnings = appendWarning(out.Warnings, "some programs have partial catalog coverage")
		}
		for _, entry := range program.Targets {
			authorized = append(authorized, &catalogCandidate{entry: entry})
		}
		for _, entry := range program.Unconfirmed {
			unconfirmed = append(unconfirmed, &catalogCandidate{entry: entry})
		}
	}
	if out.UnregisteredFiles > 0 {
		out.Warnings = appendWarning(out.Warnings, "some visible active documents lack reviewed platform/target registration; coverage is partial")
	}
	orderCatalogCandidates(authorized, args.Order)
	if len(authorized) > limit {
		out.Truncated = true
		authorized = authorized[:limit]
	}
	for rank, candidate := range authorized {
		candidate.entry.Rank = rank + 1
		out.Targets = append(out.Targets, candidate.entry)
	}
	if args.IncludeUnconfirmed {
		sort.Slice(unconfirmed, func(i, j int) bool { return catalogKey(unconfirmed[i]) < catalogKey(unconfirmed[j]) })
		if len(unconfirmed) > limit {
			out.Truncated = true
			unconfirmed = unconfirmed[:limit]
		}
		for _, candidate := range unconfirmed {
			out.Unconfirmed = append(out.Unconfirmed, candidate.entry)
		}
	}
	return fitCatalogResponse(out), nil
}

func catalogAssetsFromPayload(payload map[string]*qdrant.Value) ([]normalizedAsset, bool) {
	seen := map[string]normalizedAsset{}
	clipped := false
	for _, field := range []string{"observed_targets", "asset_refs"} {
		for _, raw := range payloadStringList(payload, field) {
			if len(seen) >= maxCatalogAssetsPerDocument {
				clipped = true
				break
			}
			asset, err := normalizeConcreteTarget(raw)
			if err == nil {
				seen[asset.Type+"\x00"+asset.Value] = asset
			}
		}
	}
	out := make([]normalizedAsset, 0, len(seen))
	for _, asset := range seen {
		out = append(out, asset)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type+":"+out[i].Value < out[j].Type+":"+out[j].Value })
	return out, clipped
}

func completeCatalogCandidate(candidate *catalogCandidate, manifest *scopeManifest, revision string) {
	entry := &candidate.entry
	entry.Scope = TargetScopeSummary{ScopeRevision: revision, ActionAllowed: false}
	keys := make([]string, 0, len(candidate.assets))
	for key := range candidate.assets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		asset := candidate.assets[key]
		status := effectiveScopeStatus(manifest, []normalizedAsset{asset})
		entry.ObservedAssets = append(entry.ObservedAssets, CatalogAsset{Type: asset.Type, Value: asset.Value,
			ScopeStatus: status, ActionAllowed: status == "authorized"})
		switch status {
		case "authorized":
			entry.Scope.AuthorizedAssets++
		case "out_of_scope":
			entry.Scope.ExcludedAssets++
		default:
			entry.Scope.UnknownAssets++
		}
	}
	if len(entry.ObservedAssets) > 20 {
		entry.ObservedAssets = entry.ObservedAssets[:20]
	}
	switch {
	case entry.Scope.AuthorizedAssets > 0 && entry.Scope.ExcludedAssets+entry.Scope.UnknownAssets > 0:
		entry.Scope.Status = "mixed"
	case entry.Scope.AuthorizedAssets > 0:
		entry.Scope.Status = "authorized"
	case entry.Scope.ExcludedAssets > 0 && entry.Scope.UnknownAssets == 0:
		entry.Scope.Status = "out_of_scope"
	default:
		entry.Scope.Status = "unknown"
	}
	// Coverage counts active distinct documents tied to at least one approved
	// observed asset. A mixed source cannot gain rank from excluded-only data.
	sources, paths := map[string]bool{}, map[string]bool{}
	for _, doc := range candidate.documents {
		approved := false
		for _, asset := range doc.assets {
			if effectiveScopeStatus(manifest, []normalizedAsset{asset}) == "authorized" {
				approved = true
				break
			}
		}
		if !approved {
			continue
		}
		switch doc.kind {
		case "note":
			entry.Coverage.NoteDocuments++
		case "evidence":
			entry.Coverage.EvidenceDocuments++
		default:
			entry.Coverage.ReconDocuments++
		}
		if doc.source != "" {
			sources[doc.source] = true
		}
		paths[doc.path] = true
		if doc.latest > entry.LatestAt {
			entry.LatestAt = doc.latest
		}
	}
	entry.Coverage.DistinctSources = len(sources)
	for path := range paths {
		entry.SourcePaths = append(entry.SourcePaths, path)
	}
	sort.Strings(entry.SourcePaths)
	if len(entry.SourcePaths) > 3 {
		entry.SourcePaths = entry.SourcePaths[:3]
	}
	entry.Band = catalogBand(entry.Coverage)
	entry.Reasons = catalogReasons(entry.Coverage)
}

func catalogBand(coverage TargetCoverage) string {
	if coverage.ReconDocuments == 0 {
		return "unmapped"
	}
	if coverage.NoteDocuments+coverage.EvidenceDocuments == 0 {
		return "emerging"
	}
	return "ready"
}

func catalogReasons(coverage TargetCoverage) []string {
	if coverage.ReconDocuments == 0 {
		return []string{"no linked recon documents for approved assets"}
	}
	if coverage.NoteDocuments+coverage.EvidenceDocuments == 0 {
		return []string{"recon exists without notes or evidence"}
	}
	return []string{"recon and supporting context are linked"}
}

func catalogKey(candidate *catalogCandidate) string {
	return candidate.entry.ProgramID + ":" + candidate.entry.Platform + ":" + strings.ToLower(candidate.entry.TargetName)
}

func catalogDocumentTotal(coverage TargetCoverage) int {
	return coverage.ReconDocuments + coverage.NoteDocuments + coverage.EvidenceDocuments
}

func orderCatalogCandidates(candidates []*catalogCandidate, order string) {
	bandRank := map[string]int{"emerging": 0, "ready": 1, "unmapped": 2}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].entry, candidates[j].entry
		switch order {
		case "most_documented":
			if left.Coverage.DistinctSources != right.Coverage.DistinctSources {
				return left.Coverage.DistinctSources > right.Coverage.DistinctSources
			}
			if catalogDocumentTotal(left.Coverage) != catalogDocumentTotal(right.Coverage) {
				return catalogDocumentTotal(left.Coverage) > catalogDocumentTotal(right.Coverage)
			}
		case "needs_recon":
			if left.Coverage.ReconDocuments != right.Coverage.ReconDocuments {
				return left.Coverage.ReconDocuments < right.Coverage.ReconDocuments
			}
			if catalogDocumentTotal(left.Coverage) != catalogDocumentTotal(right.Coverage) {
				return catalogDocumentTotal(left.Coverage) < catalogDocumentTotal(right.Coverage)
			}
		default:
			if bandRank[left.Band] != bandRank[right.Band] {
				return bandRank[left.Band] < bandRank[right.Band]
			}
			if left.Coverage.DistinctSources != right.Coverage.DistinctSources {
				return left.Coverage.DistinctSources > right.Coverage.DistinctSources
			}
		}
		if left.LatestAt != right.LatestAt {
			return left.LatestAt > right.LatestAt
		}
		return catalogKey(candidates[i]) < catalogKey(candidates[j])
	})
	if order != "balanced" {
		return
	}
	groups := map[string][]*catalogCandidate{"emerging": {}, "ready": {}, "unmapped": {}}
	for _, candidate := range candidates {
		groups[candidate.entry.Band] = append(groups[candidate.entry.Band], candidate)
	}
	result := make([]*catalogCandidate, 0, len(candidates))
	for len(result) < len(candidates) {
		for _, band := range []string{"emerging", "ready", "unmapped"} {
			if len(groups[band]) == 0 {
				continue
			}
			result = append(result, groups[band][0])
			groups[band] = groups[band][1:]
		}
	}
	copy(candidates, result)
}

func fitCatalogResponse(response HiveListTargetsResponse) HiveListTargetsResponse {
	for {
		encoded, err := json.Marshal(response)
		if err == nil && len(encoded) <= maxCatalogResponseBytes {
			return response
		}
		response.Truncated = true
		response.Warnings = appendWarning(response.Warnings, "catalog response was truncated to the safety limit")
		if len(response.Unconfirmed) > 0 {
			response.Unconfirmed = response.Unconfirmed[:len(response.Unconfirmed)-1]
			continue
		}
		if len(response.Targets) > 0 {
			response.Targets = response.Targets[:len(response.Targets)-1]
			continue
		}
		return response
	}
}

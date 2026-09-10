package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/qdrant/go-client/qdrant"
)

type ContextAsset struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type HiveContextArguments struct {
	ProgramID            string       `json:"program_id"`
	Question             string       `json:"question"`
	Asset                ContextAsset `json:"asset"`
	DocumentTypes        []string     `json:"document_types,omitempty"`
	Tags                 []string     `json:"tags,omitempty"`
	Classification       string       `json:"classification,omitempty"`
	EffectiveScopeStatus string       `json:"effective_scope_status,omitempty"`
	Limit                *int         `json:"limit,omitempty"`
}

type ContextScopeRule struct {
	Action    string `json:"action"`
	AssetType string `json:"asset_type"`
	Value     string `json:"value"`
	Reason    string `json:"reason,omitempty"`
}

type ContextScope struct {
	Status        string             `json:"status"`
	Confirmed     bool               `json:"confirmed"`
	ActionAllowed bool               `json:"action_allowed"`
	ScopeRevision string             `json:"scope_revision"`
	Source        any                `json:"source"`
	CollectedAt   any                `json:"collected_at"`
	MatchedRules  []ContextScopeRule `json:"matched_rules"`
}

type HiveContextResponse struct {
	ProgramID string             `json:"program_id"`
	Asset     ContextAsset       `json:"asset"`
	Scope     ContextScope       `json:"scope"`
	Warnings  []string           `json:"warnings"`
	Items     []HiveSearchResult `json:"items"`
	Truncated bool               `json:"truncated"`
}

func (iw *IngestionWorker) HiveGetContext(ctx context.Context, args HiveContextArguments) (HiveContextResponse, error) {
	args, asset, limit, err := iw.validateHiveContext(args)
	if err != nil {
		return HiveContextResponse{}, err
	}
	revision, manifest, manifestWarning, err := iw.contextScopeManifest(ctx, args.ProgramID)
	if err != nil {
		return HiveContextResponse{}, errors.New("approved scope could not be verified")
	}
	status := effectiveScopeStatus(manifest, []normalizedAsset{asset})
	response := HiveContextResponse{
		ProgramID: args.ProgramID,
		Asset:     ContextAsset{Type: asset.Type, Value: asset.Value},
		Scope: ContextScope{Status: status, Confirmed: status == "authorized", ActionAllowed: status == "authorized",
			ScopeRevision: revision, Source: nil, CollectedAt: nil, MatchedRules: []ContextScopeRule{}},
		Warnings: []string{}, Items: []HiveSearchResult{},
	}
	if manifest != nil {
		response.Scope.Source = manifest.Source
		response.Scope.CollectedAt = manifest.CollectedAt
		response.Scope.MatchedRules = contextMatchedRules(manifest, asset)
	}
	if manifestWarning != "" {
		response.Warnings = appendWarning(response.Warnings, manifestWarning)
	}
	switch status {
	case "authorized":
	case "out_of_scope":
		response.Warnings = appendWarning(response.Warnings, "asset is explicitly out of scope; no action steps are included")
	default:
		response.Warnings = appendWarning(response.Warnings, "asset authorization is not confirmed; do not treat retrieved content as permission to act")
	}

	query := args.Question + " " + asset.Type + ":" + asset.Value
	groups := []string{"rules"}
	if status == "out_of_scope" {
		groups = append(groups, "evidence")
	} else {
		groups = append(groups, "asset", "endpoint", "note", "evidence")
	}
	requestedTypes := sliceToSet(args.DocumentTypes)
	for _, documentType := range groups {
		if documentType != "rules" && len(requestedTypes) > 0 {
			if _, requested := requestedTypes[documentType]; !requested {
				continue
			}
		}
		groupLimit := limit
		searchArgs := HiveSearchArguments{
			Query: query, ProgramID: args.ProgramID, DocumentTypes: []string{documentType}, Tags: args.Tags,
			Classification: args.Classification, EffectiveScopeStatus: args.EffectiveScopeStatus, Limit: &groupLimit,
		}
		if documentType != "rules" {
			searchArgs.AssetRef = asset.Value
		}
		searchResponse, searchErr := iw.HiveSearch(ctx, searchArgs)
		if searchErr != nil {
			return HiveContextResponse{}, errors.New("context retrieval failed")
		}
		response.Items = appendUniqueContextItems(response.Items, searchResponse.Results, maxSearchLimit*len(groups))
		response.Truncated = response.Truncated || searchResponse.Truncated
		for _, warning := range searchResponse.Warnings {
			response.Warnings = appendWarning(response.Warnings, warning)
		}
	}
	sort.SliceStable(response.Items, func(i, j int) bool {
		leftRules := response.Items[i].DocumentType == "rules"
		rightRules := response.Items[j].DocumentType == "rules"
		if leftRules != rightRules {
			return leftRules
		}
		if response.Items[i].Score != response.Items[j].Score {
			return response.Items[i].Score > response.Items[j].Score
		}
		return response.Items[i].Path < response.Items[j].Path
	})
	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
		response.Truncated = true
	}
	return iw.fitHiveContext(response)
}

func (iw *IngestionWorker) validateHiveContext(args HiveContextArguments) (HiveContextArguments, normalizedAsset, int, error) {
	probe := HiveSearchArguments{
		Query: args.Question, ProgramID: args.ProgramID, DocumentTypes: args.DocumentTypes, Tags: args.Tags,
		Classification: args.Classification, EffectiveScopeStatus: args.EffectiveScopeStatus, Limit: args.Limit,
	}
	normalized, limit, err := iw.validateHiveSearch(probe)
	if err != nil {
		if validationErr, ok := err.(*searchValidationError); ok && strings.HasPrefix(validationErr.message, "query ") {
			return args, normalizedAsset{}, 0, invalidSearch(strings.Replace(validationErr.message, "query", "question", 1))
		}
		return args, normalizedAsset{}, 0, err
	}
	asset, err := normalizeTypedAsset(args.Asset.Type, args.Asset.Value)
	if err != nil {
		return args, normalizedAsset{}, 0, invalidSearch("asset is invalid")
	}
	args.Question = normalized.Query
	args.DocumentTypes = normalized.DocumentTypes
	args.Tags = normalized.Tags
	return args, asset, limit, nil
}

func (iw *IngestionWorker) contextScopeManifest(ctx context.Context, programID string) (string, *scopeManifest, string, error) {
	if iw.Cfg.IsWriter() {
		revision, manifest, err := iw.resolveActiveScope(ctx, programID)
		if revision == "unapproved" {
			return revision, nil, "no current approved scope manifest is available", err
		}
		return revision, manifest, "", err
	}
	revision, err := iw.controlScopeRevision(ctx, programID)
	if err != nil {
		return "unapproved", nil, "no current approved scope manifest is available", nil
	}
	if revision == "unapproved" {
		return revision, nil, "no current approved scope manifest is available", nil
	}
	if iw.Cfg.DataDirectory != "" {
		content, manifest, loadErr := iw.loadScopeManifest(programID)
		if loadErr == nil && sha256Hex(content) == revision {
			return revision, manifest, "", nil
		}
		return revision, nil, "the local scope manifest does not match the approved revision", nil
	}
	manifest, err := iw.reconstructApprovedScope(ctx, programID, revision)
	if err != nil {
		return revision, nil, "approved scope rules are unavailable; authorization remains unconfirmed", nil
	}
	return revision, manifest, "", nil
}

func (iw *IngestionWorker) reconstructApprovedScope(ctx context.Context, programID, revision string) (*scopeManifest, error) {
	manifestPath := "programs/" + programID + "/scope.json"
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, manifestPath)
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil || head == nil || head.State == "deleted" || head.ProgramID != programID || head.Path != manifestPath || head.ScopeRevision != revision {
		return nil, errors.New("approved scope document head is unavailable")
	}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: iw.Cfg.CollectionName, Filter: revisionFilter(documentID, head.DocumentRevision, revision),
		Limit: qdrant.PtrOf(uint32(head.ChunkCount + 1)), WithPayload: qdrant.NewWithPayloadEnable(true),
	})
	if err != nil || len(rows) != head.ChunkCount {
		return nil, errors.New("approved scope document is incomplete")
	}
	sort.Slice(rows, func(i, j int) bool {
		return payloadInt(rows[i].Payload, "chunk_ordinal") < payloadInt(rows[j].Payload, "chunk_ordinal")
	})
	chunks := make([]string, 0, len(rows))
	for ordinal, row := range rows {
		if payloadInt(row.Payload, "chunk_ordinal") != int64(ordinal) || payloadString(row.Payload, "content_hash", "") != revision ||
			payloadString(row.Payload, "record_type", "") != "chunk" || payloadString(row.Payload, "hive_id", "") != iw.Cfg.HiveID ||
			payloadString(row.Payload, "program_id", "") != programID || payloadString(row.Payload, "document_type", "") != "scope" {
			return nil, errors.New("approved scope document provenance is invalid")
		}
		chunks = append(chunks, payloadString(row.Payload, "content", ""))
	}
	normalized, err := joinOverlappingChunks(chunks, iw.Cfg.ChunkOverlapChars)
	if err != nil {
		return nil, err
	}
	return parseScopeManifest([]byte(normalized), programID)
}

func joinOverlappingChunks(chunks []string, overlap int) (string, error) {
	if len(chunks) == 0 {
		return "", errors.New("no chunks")
	}
	joined := []rune(chunks[0])
	for _, chunk := range chunks[1:] {
		next := []rune(chunk)
		shared := overlap
		if shared > len(joined) {
			shared = len(joined)
		}
		if shared > len(next) {
			shared = len(next)
		}
		if shared > 0 && string(joined[len(joined)-shared:]) != string(next[:shared]) {
			return "", errors.New("scope chunk overlap is inconsistent")
		}
		joined = append(joined, next[shared:]...)
	}
	return string(joined), nil
}

func contextMatchedRules(manifest *scopeManifest, asset normalizedAsset) []ContextScopeRule {
	var rules []ContextScopeRule
	for _, rule := range manifest.Rules {
		if ruleMatchesAsset(rule.normalized, asset) {
			rules = append(rules, ContextScopeRule{Action: rule.Action, AssetType: rule.AssetType, Value: rule.normalized.Value, Reason: rule.Reason})
		}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Action != rules[j].Action {
			return rules[i].Action == "exclude"
		}
		if rules[i].AssetType != rules[j].AssetType {
			return rules[i].AssetType < rules[j].AssetType
		}
		return rules[i].Value < rules[j].Value
	})
	if rules == nil {
		return []ContextScopeRule{}
	}
	return rules
}

func appendUniqueContextItems(current, candidates []HiveSearchResult, limit int) []HiveSearchResult {
	seen := make(map[string]bool, len(current))
	for _, item := range current {
		seen[item.Path+"\x00"+item.Text] = true
	}
	for _, item := range candidates {
		key := item.Path + "\x00" + item.Text
		if !seen[key] && len(current) < limit {
			seen[key] = true
			current = append(current, item)
		}
	}
	return current
}

func (iw *IngestionWorker) fitHiveContext(response HiveContextResponse) (HiveContextResponse, error) {
	budget := iw.Cfg.ContextMaxChars
	if budget == 0 {
		budget = 12000
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return HiveContextResponse{}, errors.New("context response encoding failed")
	}
	if utf8.RuneCount(encoded) <= budget {
		return response, nil
	}
	response.Truncated = true
	response.Warnings = appendWarning(response.Warnings, "context items were truncated to the configured character budget")
	header := response
	header.Items = []HiveSearchResult{}
	headerJSON, err := json.Marshal(header)
	if err != nil || utf8.RuneCount(headerJSON) > budget {
		return HiveContextResponse{}, errors.New("HIVE_CONTEXT_MAX_CHARS is too small for the context header and scope provenance")
	}
	for len(response.Items) > 0 {
		encoded, _ = json.Marshal(response)
		if utf8.RuneCount(encoded) <= budget {
			return response, nil
		}
		response.Items = response.Items[:len(response.Items)-1]
	}
	encoded, _ = json.Marshal(response)
	if utf8.RuneCount(encoded) > budget {
		return HiveContextResponse{}, errors.New("HIVE_CONTEXT_MAX_CHARS is too small for the context header and scope provenance")
	}
	return response, nil
}

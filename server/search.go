package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/qdrant/go-client/qdrant"
)

const (
	defaultSearchLimit     = 8
	maxSearchLimit         = 20
	maxSearchQueryRunes    = 2000
	maxSearchTextBytes     = 48 * 1024
	maxSearchResponseBytes = 60 * 1024
)

type HiveSearchArguments struct {
	Query                string   `json:"query"`
	ProgramID            string   `json:"program_id"`
	DocumentTypes        []string `json:"document_types,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Classification       string   `json:"classification,omitempty"`
	EffectiveScopeStatus string   `json:"effective_scope_status,omitempty"`
	Limit                *int     `json:"limit,omitempty"`
	AssetRef             string   `json:"-"`
}

type HiveSearchResult struct {
	Text                 string  `json:"text"`
	Score                float32 `json:"score"`
	Path                 string  `json:"path"`
	Source               any     `json:"source"`
	CollectedAt          any     `json:"collected_at"`
	DocumentType         string  `json:"document_type"`
	EffectiveScopeStatus string  `json:"effective_scope_status"`
	Classification       string  `json:"classification"`
	UntrustedContent     bool    `json:"untrusted_content"`
}

type HiveSearchResponse struct {
	Results   []HiveSearchResult `json:"results"`
	Warnings  []string           `json:"warnings"`
	Truncated bool               `json:"truncated"`
}

type searchValidationError struct{ message string }

func (e *searchValidationError) Error() string { return e.message }

func isSearchValidationError(err error) bool {
	var validationErr *searchValidationError
	return errors.As(err, &validationErr)
}

func invalidSearch(message string) error { return &searchValidationError{message: message} }

func (iw *IngestionWorker) HiveSearch(ctx context.Context, args HiveSearchArguments) (HiveSearchResponse, error) {
	args, limit, err := iw.validateHiveSearch(args)
	if err != nil {
		return HiveSearchResponse{}, err
	}
	scopeRevision, err := iw.controlScopeRevision(ctx, args.ProgramID)
	if err != nil {
		return HiveSearchResponse{}, errors.New("active scope revision is unavailable")
	}
	filter := iw.hiveSearchFilter(args, scopeRevision)
	candidateLimit := uint64(limit * 4)
	if candidateLimit < 32 {
		candidateLimit = 32
	}
	if candidateLimit > 80 {
		candidateLimit = 80
	}
	points, err := iw.queryVariant(ctx, args.Query, filter, candidateLimit)
	if err != nil {
		return HiveSearchResponse{}, errors.New("semantic retrieval failed")
	}
	points = iw.enforceSearchBoundary(points, args, scopeRevision)
	active, err := iw.filterActiveCandidates(ctx, points)
	if err != nil {
		return HiveSearchResponse{}, errors.New("active revision validation failed")
	}
	unique := deduplicateSearchPoints(active)
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].Score != unique[j].Score {
			return unique[i].Score > unique[j].Score
		}
		leftPath := payloadString(unique[i].Payload, "path", "")
		rightPath := payloadString(unique[j].Payload, "path", "")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return payloadInt(unique[i].Payload, "chunk_ordinal") < payloadInt(unique[j].Payload, "chunk_ordinal")
	})
	response := HiveSearchResponse{Results: []HiveSearchResult{}, Warnings: []string{}}
	if len(unique) > limit || len(points) >= int(candidateLimit) {
		response.Truncated = true
	}
	if len(unique) > limit {
		unique = unique[:limit]
	}
	remainingText := maxSearchTextBytes
	for _, point := range unique {
		text := payloadString(point.Payload, "content", "")
		if len(text) > remainingText {
			text = truncateUTF8Bytes(text, remainingText)
			response.Truncated = true
			response.Warnings = appendWarning(response.Warnings, "result text was truncated to the response safety limit")
		}
		remainingText -= len(text)
		response.Results = append(response.Results, HiveSearchResult{
			Text: text, Score: point.Score, Path: payloadString(point.Payload, "path", ""),
			Source: optionalPayloadString(point.Payload, "source"), CollectedAt: optionalPayloadString(point.Payload, "collected_at"),
			DocumentType:         payloadString(point.Payload, "document_type", "unknown"),
			EffectiveScopeStatus: payloadString(point.Payload, "effective_scope_status", "unknown"),
			Classification:       payloadString(point.Payload, "classification", "unknown"), UntrustedContent: true,
		})
		if remainingText == 0 {
			break
		}
	}
	if len(response.Results) < len(unique) {
		response.Truncated = true
		response.Warnings = appendWarning(response.Warnings, "additional results were omitted by the response safety limit")
	}
	return fitSearchResponse(response), nil
}

func (iw *IngestionWorker) validateHiveSearch(args HiveSearchArguments) (HiveSearchArguments, int, error) {
	args.Query = strings.TrimSpace(args.Query)
	if count := utf8.RuneCountInString(args.Query); count < 1 || count > maxSearchQueryRunes {
		return args, 0, invalidSearch("query must contain between 1 and 2000 characters")
	}
	if !validIdentifier(args.ProgramID, 64) {
		return args, 0, invalidSearch("program_id is invalid")
	}
	limit := defaultSearchLimit
	if args.Limit != nil {
		limit = *args.Limit
		if limit < 1 || limit > maxSearchLimit {
			return args, 0, invalidSearch("limit must be between 1 and 20")
		}
	}
	var err error
	args.DocumentTypes, err = validateSearchList(args.DocumentTypes, "document_types", 7, func(value string) bool { return allowedDocumentTypes[value] })
	if err != nil {
		return args, 0, err
	}
	args.Tags, err = validateSearchList(args.Tags, "tags", 64, func(value string) bool { return value != "" && len(value) <= 100 })
	if err != nil {
		return args, 0, err
	}
	if args.Classification != "" && !allowedClassifications[args.Classification] {
		return args, 0, invalidSearch("classification is invalid")
	}
	if args.Classification == "restricted" && iw.Cfg.MaxClassification != "restricted" {
		return args, 0, invalidSearch("classification exceeds the configured maximum")
	}
	if args.EffectiveScopeStatus != "" && !allowedScopeStatuses[args.EffectiveScopeStatus] {
		return args, 0, invalidSearch("effective_scope_status is invalid")
	}
	return args, limit, nil
}

func validateSearchList(values []string, field string, maxItems int, valid func(string) bool) ([]string, error) {
	if len(values) > maxItems {
		return nil, invalidSearch(fmt.Sprintf("%s contains too many values", field))
	}
	seen := make(map[string]bool, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !valid(value) {
			return nil, invalidSearch(fmt.Sprintf("%s contains an invalid value", field))
		}
		if seen[value] {
			return nil, invalidSearch(fmt.Sprintf("%s contains duplicate values", field))
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func (iw *IngestionWorker) hiveSearchFilter(args HiveSearchArguments, scopeRevision string) *qdrant.Filter {
	must := []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "chunk"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
		qdrant.NewMatchKeyword("program_id", args.ProgramID), qdrant.NewMatchKeyword("scope_revision", scopeRevision),
	}
	if iw.Cfg.MaxClassification == "restricted" {
		must = append(must, qdrant.NewMatchKeywords("classification", "internal", "restricted"))
	} else {
		must = append(must, qdrant.NewMatchKeyword("classification", "internal"))
	}
	if args.Classification != "" {
		must = append(must, qdrant.NewMatchKeyword("classification", args.Classification))
	}
	if len(args.DocumentTypes) > 0 {
		must = append(must, qdrant.NewMatchKeywords("document_type", args.DocumentTypes...))
	}
	for _, tag := range args.Tags {
		must = append(must, qdrant.NewMatchKeyword("tags", tag))
	}
	if args.EffectiveScopeStatus != "" {
		must = append(must, qdrant.NewMatchKeyword("effective_scope_status", args.EffectiveScopeStatus))
	}
	if args.AssetRef != "" {
		must = append(must, qdrant.NewMatchKeyword("asset_refs", args.AssetRef))
	}
	return &qdrant.Filter{Must: must}
}

func (iw *IngestionWorker) enforceSearchBoundary(points []*qdrant.ScoredPoint, args HiveSearchArguments, scopeRevision string) []*qdrant.ScoredPoint {
	filtered := make([]*qdrant.ScoredPoint, 0, len(points))
	for _, point := range points {
		payload := point.Payload
		classification := payloadString(payload, "classification", "unknown")
		allowedClassification := classification == "internal" || (classification == "restricted" && iw.Cfg.MaxClassification == "restricted")
		if !allowedClassification || (args.Classification != "" && classification != args.Classification) {
			continue
		}
		if payloadString(payload, "record_type", "") != "chunk" || payloadString(payload, "hive_id", "") != iw.Cfg.HiveID ||
			payloadString(payload, "program_id", "") != args.ProgramID || payloadString(payload, "scope_revision", "") != scopeRevision {
			continue
		}
		retrievedPath := filepath.ToSlash(payloadString(payload, "path", ""))
		if filepath.IsAbs(retrievedPath) || strings.Contains(retrievedPath, "\\") || retrievedPath != filepath.ToSlash(filepath.Clean(retrievedPath)) ||
			!strings.HasPrefix(retrievedPath, "programs/"+args.ProgramID+"/") {
			continue
		}
		if len(args.DocumentTypes) > 0 && !containsString(args.DocumentTypes, payloadString(payload, "document_type", "unknown")) {
			continue
		}
		payloadTags := sliceToSet(payloadStringList(payload, "tags"))
		allTags := true
		for _, tag := range args.Tags {
			if _, found := payloadTags[tag]; !found {
				allTags = false
				break
			}
		}
		if !allTags || (args.EffectiveScopeStatus != "" && payloadString(payload, "effective_scope_status", "unknown") != args.EffectiveScopeStatus) {
			continue
		}
		if args.AssetRef != "" && !containsString(payloadStringList(payload, "asset_refs"), args.AssetRef) {
			continue
		}
		filtered = append(filtered, point)
	}
	return filtered
}

func deduplicateSearchPoints(points []*qdrant.ScoredPoint) []*qdrant.ScoredPoint {
	byInterval := make(map[string]*qdrant.ScoredPoint, len(points))
	for _, point := range points {
		key := payloadString(point.Payload, "document_id", "") + "\x00" + fmt.Sprint(payloadInt(point.Payload, "chunk_ordinal"))
		if existing, found := byInterval[key]; !found || point.Score > existing.Score {
			byInterval[key] = point
		}
	}
	out := make([]*qdrant.ScoredPoint, 0, len(byInterval))
	for _, point := range byInterval {
		out = append(out, point)
	}
	return out
}

func optionalPayloadString(payload map[string]*qdrant.Value, key string) any {
	value := payload[key]
	if value == nil || value.GetKind() == nil {
		return nil
	}
	if _, isNull := value.GetKind().(*qdrant.Value_NullValue); isNull {
		return nil
	}
	text := value.GetStringValue()
	if text == "" {
		return nil
	}
	return text
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func appendWarning(warnings []string, warning string) []string {
	for _, existing := range warnings {
		if existing == warning {
			return warnings
		}
	}
	return append(warnings, warning)
}

func fitSearchResponse(response HiveSearchResponse) HiveSearchResponse {
	for {
		encoded, err := json.Marshal(response)
		if err == nil && len(encoded) <= maxSearchResponseBytes {
			return response
		}
		response.Truncated = true
		response.Warnings = appendWarning(response.Warnings, "response was truncated to the total safety limit")
		if len(response.Results) == 0 {
			return response
		}
		last := len(response.Results) - 1
		if response.Results[last].Text != "" {
			overflow := len(encoded) - maxSearchResponseBytes
			keep := len(response.Results[last].Text) - overflow - 128
			if keep < 0 {
				keep = 0
			}
			response.Results[last].Text = truncateUTF8Bytes(response.Results[last].Text, keep)
			continue
		}
		response.Results = response.Results[:last]
	}
}

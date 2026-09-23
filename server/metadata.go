package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var allowedDocumentTypes = map[string]bool{
	"scope": true, "rules": true, "asset": true, "endpoint": true,
	"note": true, "evidence": true, "unknown": true,
}

var allowedScopeStatuses = map[string]bool{
	"authorized": true, "out_of_scope": true, "unknown": true,
}

var allowedClassifications = map[string]bool{
	"internal": true, "restricted": true, "unknown": true,
}

type reconMetadata struct {
	DocumentType    string
	Platform        string
	TargetName      string
	ClaimedScope    string
	Classification  string
	Source          any
	CollectedAt     any
	Tags            []string
	AssetRefs       []normalizedAsset
	AssetRefPayload []string
	ObservedTargets []string
}

func extractReconMetadata(relPath, pathProgramID string, content []byte) (reconMetadata, error) {
	meta := reconMetadata{DocumentType: inferDocumentType(relPath), ClaimedScope: "unknown", Classification: "unknown"}
	values := map[string]any{}
	var err error
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".md":
		values, err = parseFrontMatter(content)
	case ".json":
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		var decoded any
		if err = decoder.Decode(&decoded); err != nil {
			return meta, fmt.Errorf("invalid JSON metadata: %w", err)
		}
		if object, ok := decoded.(map[string]any); ok {
			values = object
		}
	}
	if err != nil {
		return meta, err
	}
	if raw, exists := values["program_id"]; exists {
		claimedProgram, ok := raw.(string)
		if !ok || strings.TrimSpace(claimedProgram) != pathProgramID {
			return meta, errors.New("program_id in document does not match programs/<program_id>/ path")
		}
	}
	if raw, exists := values["platform"]; exists {
		value, ok := raw.(string)
		if !ok || !validIdentifier(value, 64) {
			return meta, errors.New("platform must be a lowercase identifier")
		}
		meta.Platform = value
	}
	if raw, exists := values["target_name"]; exists {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("target_name must be a project name")
		}
		meta.TargetName = value
	}
	if raw, exists := values["document_type"]; exists {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("document_type must be a string")
		}
		value = strings.TrimSpace(value)
		value = strings.ToLower(value)
		if !allowedDocumentTypes[value] {
			return meta, fmt.Errorf("invalid document_type %q", value)
		}
		meta.DocumentType = value
	}
	if raw, exists := values["claimed_scope_status"]; exists {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("claimed_scope_status must be a string")
		}
		value = strings.TrimSpace(value)
		value = strings.ToLower(value)
		if !allowedScopeStatuses[value] {
			return meta, fmt.Errorf("invalid claimed_scope_status %q", value)
		}
		meta.ClaimedScope = value
	}
	if raw, exists := values["classification"]; exists {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("classification must be a string")
		}
		value = strings.TrimSpace(value)
		value = strings.ToLower(value)
		if !allowedClassifications[value] {
			return meta, fmt.Errorf("invalid classification %q", value)
		}
		meta.Classification = value
	}
	if raw, exists := values["source"]; exists {
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return meta, errors.New("source must be a non-empty string")
		}
		meta.Source = strings.TrimSpace(value)
	}
	if raw, exists := values["collected_at"]; exists {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("collected_at must be an RFC 3339 string")
		}
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return meta, errors.New("collected_at must be an RFC 3339 string")
		}
		meta.CollectedAt = parsed.UTC().Format(time.RFC3339)
	}
	if raw, exists := values["tags"]; exists {
		items, listErr := metadataStringList(raw, "tags")
		if listErr != nil {
			return meta, listErr
		}
		seen := make(map[string]bool)
		for _, item := range items {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				return meta, errors.New("tags cannot contain an empty value")
			}
			seen[item] = true
		}
		for item := range seen {
			meta.Tags = append(meta.Tags, item)
		}
		sort.Strings(meta.Tags)
	}
	if raw, exists := values["asset_refs"]; exists {
		items, listErr := metadataStringList(raw, "asset_refs")
		if listErr != nil {
			return meta, listErr
		}
		seen := make(map[string]bool)
		for _, item := range items {
			asset, normalizeErr := normalizeAssetReference(item)
			if normalizeErr != nil {
				return meta, fmt.Errorf("invalid asset_ref: %w", normalizeErr)
			}
			key := asset.Type + "\x00" + asset.Value
			if !seen[key] {
				seen[key] = true
				meta.AssetRefs = append(meta.AssetRefs, asset)
			}
		}
		sort.Slice(meta.AssetRefs, func(i, j int) bool {
			if meta.AssetRefs[i].Type == meta.AssetRefs[j].Type {
				return meta.AssetRefs[i].Value < meta.AssetRefs[j].Value
			}
			return meta.AssetRefs[i].Type < meta.AssetRefs[j].Type
		})
		for _, asset := range meta.AssetRefs {
			meta.AssetRefPayload = append(meta.AssetRefPayload, asset.Value)
		}
	}
	if raw, exists := values["observed_targets"]; exists {
		items, listErr := metadataStringList(raw, "observed_targets")
		if listErr != nil || len(items) > 5000 {
			return meta, errors.New("observed_targets must be a bounded list of concrete assets")
		}
		seen := map[string]bool{}
		for _, item := range items {
			asset, normalizeErr := normalizeConcreteTarget(item)
			if normalizeErr != nil || item != asset.Type+":"+asset.Value {
				return meta, errors.New("observed_targets must contain canonical host:, ip: or url_prefix: assets")
			}
			if !seen[item] {
				seen[item] = true
				meta.ObservedTargets = append(meta.ObservedTargets, item)
			}
		}
		sort.Strings(meta.ObservedTargets)
	}
	if (meta.Platform == "") != (meta.TargetName == "") {
		return meta, errors.New("platform and target_name must be registered together")
	}
	if meta.TargetName != "" && !validTargetName(meta.TargetName, meta.DocumentType) {
		return meta, errors.New("target_name must be a valid project name, or @program for scope/rules")
	}
	if len(meta.ObservedTargets) > 0 && meta.TargetName == "" {
		return meta, errors.New("observed_targets require platform and target_name registration")
	}
	return meta, nil
}

// Observed assets are concrete. Wildcards and ranges remain scope rules only.
func normalizeConcreteTarget(value string) (normalizedAsset, error) {
	asset, err := normalizeAssetReference(value)
	if err != nil {
		return normalizedAsset{}, err
	}
	if asset.Type != "host" && asset.Type != "ip" && asset.Type != "url_prefix" {
		return normalizedAsset{}, errors.New("primary target must be a host, IP or URL prefix")
	}
	return asset, nil
}

func validTargetName(value, documentType string) bool {
	if value == "@program" {
		return documentType == "scope" || documentType == "rules"
	}
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 120 && !containsControl(value)
}

func inferDocumentType(relPath string) string {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(relPath)), "/")
	base := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
	if base == "scope" {
		return "scope"
	}
	if base == "rules" {
		return "rules"
	}
	for _, part := range append(parts[2:len(parts)-1], base) {
		switch part {
		case "asset", "assets":
			return "asset"
		case "endpoint", "endpoints":
			return "endpoint"
		case "note", "notes":
			return "note"
		case "evidence":
			return "evidence"
		}
	}
	return "unknown"
}

func metadataStringList(raw any, field string) ([]string, error) {
	switch value := raw.(type) {
	case []string:
		return value, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings", field)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a list of strings", field)
	}
}

// parseFrontMatter intentionally accepts only the flat metadata subset used by
// the Hive contract. Complex YAML is rejected instead of being interpreted
// differently by different parsers.
func parseFrontMatter(content []byte) (map[string]any, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]any{}, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, errors.New("unterminated Markdown front matter")
	}
	out := make(map[string]any)
	for i := 1; i < end; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid front matter line %d", i+1)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if _, duplicate := out[key]; duplicate {
			return nil, fmt.Errorf("duplicate front matter key %q", key)
		}
		if value == "" {
			var list []any
			for i+1 < end && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "-") {
				i++
				item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "-"))
				if item == "" {
					return nil, fmt.Errorf("empty list item for %s", key)
				}
				list = append(list, unquoteMetadata(item))
			}
			out[key] = list
			continue
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			inner := strings.TrimSpace(value[1 : len(value)-1])
			var list []any
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					list = append(list, unquoteMetadata(strings.TrimSpace(item)))
				}
			}
			out[key] = list
		} else {
			out[key] = unquoteMetadata(value)
		}
	}
	return out, nil
}

func unquoteMetadata(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

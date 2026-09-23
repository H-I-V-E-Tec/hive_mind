package server

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Extract only whole fields in recognized recon formats. Free prose is never
// interpreted as an asset, and observations never approve scope.
func extractObservedTargets(doc ConvertedDocument, explicit []string) ([]string, error) {
	seen := map[string]bool{}
	add := func(raw string, fromSource bool) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		asset, err := normalizeConcreteTarget(raw)
		if err != nil {
			if fromSource {
				return nil
			}
			return errors.New("--observed-target must be a concrete host, IP or URL")
		}
		// A lone word in a text file is too ambiguous to infer as a host.
		if fromSource && asset.Type == "host" && !strings.Contains(asset.Value, ".") {
			return nil
		}
		canonical := asset.Type + ":" + asset.Value
		seen[canonical] = true
		if len(seen) > 5000 {
			return errors.New("too many observed targets; split the source")
		}
		return nil
	}
	for _, value := range explicit {
		if err := add(value, false); err != nil {
			return nil, err
		}
	}
	for _, block := range doc.Blocks {
		switch doc.SourceFormat {
		case "txt":
			for _, line := range strings.Split(block.Text, "\n") {
				if err := add(line, true); err != nil {
					return nil, err
				}
			}
		case "csv", "tsv":
			var row convertedTableRow
			if err := json.Unmarshal([]byte(block.Text), &row); err != nil {
				return nil, errors.New("invalid converted table observation")
			}
			for index, column := range row.Columns {
				if index >= len(row.Values) {
					break
				}
				if isAssetColumn(column) {
					if err := add(row.Values[index], true); err != nil {
						return nil, err
					}
				}
			}
		case "json", "jsonl", "ndjson":
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(block.Text), &object); err != nil {
				continue
			}
			for field, encoded := range object {
				if !isAssetColumn(field) {
					continue
				}
				var value string
				if json.Unmarshal(encoded, &value) == nil {
					if err := add(value, true); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func isAssetColumn(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "hostname", "ip", "url", "target":
		return true
	default:
		return false
	}
}

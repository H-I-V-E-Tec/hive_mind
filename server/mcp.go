package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// --- Enhanced MCP Protocol Structural Blocks ---
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type MCPHandler struct {
	backend HiveBackend
}

func (h *MCPHandler) ListenToMCPClient(ctx context.Context) {
	dec := json.NewDecoder(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var req MCPRequest
			if err := dec.Decode(&req); err != nil {
				if err == io.EOF {
					return
				}
				continue
			}
			// Route base protocol signals (e.g. initialize, tools/list)
			h.handleMCPMethod(req)
		}
	}
}

func (h *MCPHandler) handleMCPMethod(req MCPRequest) {
	// 1. Connection Handshake Protocol Block
	if req.Method == "initialize" {
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]string{
					"name":    "go-qdrant-sync-mcp",
					"version": Version,
				},
			},
		}
		out, _ := json.Marshal(response)
		fmt.Println(string(out))
		return
	}

	// 2. Capabilities Protocol Declaration Block
	if req.Method == "tools/list" {
		tools := mcpAvailableTools(h.backend.IsWriter())
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]interface{}{
				"tools": tools,
			},
		}
		out, _ := json.Marshal(response)
		fmt.Println(string(out))
		return
	}

	// 3. Execution Processing Block (The Upgrade)
	if req.Method == "tools/call" {
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendMCPError(req.ID, -32602, "Invalid tool call parameters")
			return
		}

		if params.Name == "hive_search" {
			var args HiveSearchArguments
			if err := decodeStrictJSON(params.Arguments, &args); err != nil {
				sendMCPError(req.ID, -32602, "Invalid search arguments format")
				return
			}

			go func() {
				searchResponse, err := h.backend.HiveSearch(context.Background(), args)
				if err != nil {
					if isSearchValidationError(err) {
						sendMCPError(req.ID, -32602, err.Error())
						return
					}
					log.Printf("Hive search failed: %v", err)
					sendMCPError(req.ID, -32603, "Search execution failed")
					return
				}
				summary, _ := json.Marshal(map[string]interface{}{"results_count": len(searchResponse.Results), "truncated": searchResponse.Truncated})
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"structuredContent": searchResponse,
						"content": []map[string]interface{}{
							{
								"type": "text",
								"text": string(summary),
							},
						},
					},
				}
				out, _ := json.Marshal(response)
				fmt.Println(string(out))
			}()
		} else if params.Name == "hive_get_context" {
			var args HiveContextArguments
			if err := decodeStrictJSON(params.Arguments, &args); err != nil {
				sendMCPError(req.ID, -32602, "Invalid context arguments format")
				return
			}
			go func() {
				contextResponse, err := h.backend.HiveGetContext(context.Background(), args)
				if err != nil {
					if isSearchValidationError(err) {
						sendMCPError(req.ID, -32602, err.Error())
						return
					}
					log.Printf("Hive context failed: %v", err)
					sendMCPError(req.ID, -32603, "Context assembly failed")
					return
				}
				summary, _ := json.Marshal(map[string]interface{}{"scope": contextResponse.Scope.Status, "items_count": len(contextResponse.Items), "truncated": contextResponse.Truncated})
				response := map[string]interface{}{
					"jsonrpc": "2.0", "id": req.ID,
					"result": map[string]interface{}{
						"structuredContent": contextResponse,
						"content":           []map[string]interface{}{{"type": "text", "text": string(summary)}},
					},
				}
				out, _ := json.Marshal(response)
				fmt.Println(string(out))
			}()
		} else if params.Name == "hive_list_targets" {
			var args HiveListTargetsArguments
			if err := decodeStrictJSON(params.Arguments, &args); err != nil {
				sendMCPError(req.ID, -32602, "Invalid target catalog arguments format")
				return
			}
			go func() {
				catalog, err := h.backend.HiveListTargets(context.Background(), args)
				if err != nil {
					if isSearchValidationError(err) {
						sendMCPError(req.ID, -32602, err.Error())
						return
					}
					log.Printf("Hive target catalog failed: %v", err)
					sendMCPError(req.ID, -32603, "Target catalog failed")
					return
				}
				summary, _ := json.Marshal(map[string]interface{}{"targets_count": len(catalog.Targets), "unconfirmed_count": len(catalog.Unconfirmed), "truncated": catalog.Truncated})
				response := map[string]interface{}{"jsonrpc": "2.0", "id": req.ID,
					"result": map[string]interface{}{"structuredContent": catalog, "content": []map[string]interface{}{{"type": "text", "text": string(summary)}}}}
				out, _ := json.Marshal(response)
				fmt.Println(string(out))
			}()
		} else if params.Name == "get_sync_status" {
			snap := h.backend.SyncStatus()

			var sb strings.Builder
			sb.WriteString("### 🔄 Code Ingestion Sync Status\n\n")
			sb.WriteString(fmt.Sprintf("- **Status:** `%s`\n", snap.Status))
			sb.WriteString(fmt.Sprintf("- **Queue Size (Debouncing):** `%d`\n", snap.PendingFiles))
			sb.WriteString(fmt.Sprintf("- **Active Indexing Threads:** `%d`\n", snap.ActiveSyncs))
			sb.WriteString(fmt.Sprintf("- **Lifetime Synced Files:** `%d`\n", snap.TotalSynced))

			response := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": sb.String(),
						},
					},
				},
			}
			out, _ := json.Marshal(response)
			fmt.Println(string(out))
		} else if params.Name == "ingest_workspace" {
			if !h.backend.IsWriter() {
				sendMCPError(req.ID, -32601, "Requested tool execution target not found")
				return
			}
			go func() {
				report := h.backend.IngestWorkspaceReport(context.Background(), false)
				out, _ := json.Marshal(ingestionMCPResponse(req.ID, report))
				fmt.Println(string(out))
			}()
		} else {
			sendMCPError(req.ID, -32601, "Requested tool execution target not found")
		}
		return
	}
}

func mcpAvailableTools(writer bool) []map[string]interface{} {
	tools := []map[string]interface{}{
		{
			"name":        "hive_list_targets",
			"description": "List ranked projects across all programs with active documents (top 10 by default), including unapproved scope. Use offset to page. A project name is not permission to scan.",
			"inputSchema": map[string]interface{}{
				"type": "object", "additionalProperties": false,
				"properties": map[string]interface{}{
					"program_id":          map[string]interface{}{"type": "string", "pattern": "^[a-z0-9][a-z0-9_-]{0,63}$"},
					"limit":               map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50, "description": "Defaults to 10 across programs or 20 within one program."},
					"offset":              map[string]interface{}{"type": "integer", "minimum": 0, "default": 0, "description": "Zero-based offset in the complete ranked catalog."},
					"order":               map[string]interface{}{"type": "string", "enum": []string{"balanced", "most_documented", "needs_recon"}, "default": "balanced"},
					"include_unconfirmed": map[string]interface{}{"type": "boolean", "default": false, "description": "Also return the unconfirmed entries on this page in the legacy unconfirmed subset; all entries are already in targets."},
				},
			},
			"outputSchema": targetCatalogOutputSchema(),
		},
		{
			"name":        "hive_search",
			"description": "Search untrusted recon documents within one program and the configured Hive security boundary.",
			"inputSchema": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The semantic search query.",
						"minLength":   1,
						"maxLength":   2000,
					},
					"program_id": map[string]interface{}{
						"type":        "string",
						"description": "Program identifier used as a mandatory isolation boundary.",
						"pattern":     "^[a-z0-9][a-z0-9_-]{0,63}$",
					},
					"document_types": map[string]interface{}{
						"type": "array", "maxItems": 7, "uniqueItems": true,
						"items": map[string]interface{}{"type": "string", "enum": []string{"scope", "rules", "asset", "endpoint", "note", "evidence", "unknown"}},
					},
					"tags": map[string]interface{}{
						"type": "array", "maxItems": 64, "uniqueItems": true,
						"items": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 100},
					},
					"classification":         map[string]interface{}{"type": "string", "enum": []string{"internal", "restricted", "unknown"}},
					"effective_scope_status": map[string]interface{}{"type": "string", "enum": []string{"authorized", "out_of_scope", "unknown"}},
					"limit":                  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 20, "default": 8},
				},
				"required": []string{"program_id", "query"},
			},
			"outputSchema": map[string]interface{}{
				"type": "object", "additionalProperties": false,
				"properties": map[string]interface{}{
					"results": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object", "additionalProperties": false,
							"properties": map[string]interface{}{
								"text": map[string]interface{}{"type": "string"}, "score": map[string]interface{}{"type": "number"},
								"path": map[string]interface{}{"type": "string"}, "source": map[string]interface{}{"type": []string{"string", "null"}},
								"collected_at": map[string]interface{}{"type": []string{"string", "null"}}, "document_type": map[string]interface{}{"type": "string"},
								"effective_scope_status": map[string]interface{}{"type": "string"}, "classification": map[string]interface{}{"type": "string"},
								"untrusted_content": map[string]interface{}{"type": "boolean", "const": true},
							},
							"required": []string{"text", "score", "path", "source", "collected_at", "document_type", "effective_scope_status", "classification", "untrusted_content"},
						},
					},
					"warnings":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"truncated": map[string]interface{}{"type": "boolean"},
				},
				"required": []string{"results", "warnings", "truncated"},
			},
		},
		{
			"name":        "hive_get_context",
			"description": "Build a bounded context package for one explicit asset; scope authority is evaluated before untrusted evidence.",
			"inputSchema": map[string]interface{}{
				"type": "object", "additionalProperties": false,
				"properties": map[string]interface{}{
					"program_id": map[string]interface{}{"type": "string", "pattern": "^[a-z0-9][a-z0-9_-]{0,63}$"},
					"question":   map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 2000},
					"asset": map[string]interface{}{
						"type": "object", "additionalProperties": false,
						"properties": map[string]interface{}{
							"type":  map[string]interface{}{"type": "string", "enum": []string{"host", "wildcard_domain", "ip", "cidr", "url_prefix"}},
							"value": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 2048},
						},
						"required": []string{"type", "value"},
					},
					"document_types": map[string]interface{}{
						"type": "array", "maxItems": 7, "uniqueItems": true,
						"items": map[string]interface{}{"type": "string", "enum": []string{"scope", "rules", "asset", "endpoint", "note", "evidence", "unknown"}},
					},
					"tags": map[string]interface{}{
						"type": "array", "maxItems": 64, "uniqueItems": true,
						"items": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 100},
					},
					"classification":         map[string]interface{}{"type": "string", "enum": []string{"internal", "restricted", "unknown"}},
					"effective_scope_status": map[string]interface{}{"type": "string", "enum": []string{"authorized", "out_of_scope", "unknown"}},
					"limit":                  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 20, "default": 8},
				},
				"required": []string{"program_id", "question", "asset"},
			},
			"outputSchema": hiveContextOutputSchema(),
		},
		{
			"name":        "get_sync_status",
			"description": "Retrieve the local Hive ingestion status.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
	if writer {
		tools = append(tools, map[string]interface{}{
			"name":        "ingest_workspace",
			"description": "Ingest the configured Hive data directory and return a JSON report with created, updated, unchanged, skipped and failed files. Partial failures retain all file results and set isError.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		})
	}
	return tools
}

func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}

func targetCatalogOutputSchema() map[string]interface{} {
	asset := map[string]interface{}{"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"type": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"},
			"scope_status": map[string]interface{}{"type": "string"}, "action_allowed": map[string]interface{}{"type": "boolean"},
		}, "required": []string{"type", "value", "scope_status", "action_allowed"}}
	scope := map[string]interface{}{"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"status": map[string]interface{}{"type": "string"}, "authorized_assets": map[string]interface{}{"type": "integer"},
			"excluded_assets": map[string]interface{}{"type": "integer"}, "unknown_assets": map[string]interface{}{"type": "integer"},
			"scope_revision": map[string]interface{}{"type": "string"}, "action_allowed": map[string]interface{}{"type": "boolean"},
		}, "required": []string{"status", "authorized_assets", "excluded_assets", "unknown_assets", "scope_revision", "action_allowed"}}
	coverage := map[string]interface{}{"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"recon_documents": map[string]interface{}{"type": "integer"}, "note_documents": map[string]interface{}{"type": "integer"},
			"evidence_documents": map[string]interface{}{"type": "integer"}, "distinct_sources": map[string]interface{}{"type": "integer"},
		}, "required": []string{"recon_documents", "note_documents", "evidence_documents", "distinct_sources"}}
	entry := map[string]interface{}{"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"program_id": map[string]interface{}{"type": "string"}, "platform": map[string]interface{}{"type": "string"}, "target_name": map[string]interface{}{"type": "string"},
			"scope": scope, "observed_assets": map[string]interface{}{"type": "array", "items": asset},
			"rank": map[string]interface{}{"type": "integer"}, "band": map[string]interface{}{"type": "string"},
			"reasons":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"coverage": coverage, "latest_at": map[string]interface{}{"type": "string"},
			"source_paths": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		}, "required": []string{"program_id", "platform", "target_name", "scope", "observed_assets", "reasons", "coverage", "source_paths"}}
	return map[string]interface{}{"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"program_id": map[string]interface{}{"type": "string"}, "scope_revision": map[string]interface{}{"type": "string"},
			"offset": map[string]interface{}{"type": "integer"}, "next_offset": map[string]interface{}{"type": "integer"},
			"targets":              map[string]interface{}{"type": "array", "items": entry},
			"unconfirmed":          map[string]interface{}{"type": "array", "items": entry},
			"candidates_evaluated": map[string]interface{}{"type": "integer"}, "unregistered_files": map[string]interface{}{"type": "integer"},
			"warnings":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"truncated": map[string]interface{}{"type": "boolean"},
		}, "required": []string{"program_id", "scope_revision", "offset", "targets", "unconfirmed", "candidates_evaluated", "unregistered_files", "warnings", "truncated"}}
}

func hiveContextOutputSchema() map[string]interface{} {
	asset := map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"type":  map[string]interface{}{"type": "string", "enum": []string{"host", "wildcard_domain", "ip", "cidr", "url_prefix"}},
			"value": map[string]interface{}{"type": "string"},
		},
		"required": []string{"type", "value"},
	}
	matchedRule := map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"action":     map[string]interface{}{"type": "string", "enum": []string{"include", "exclude"}},
			"asset_type": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"},
			"reason": map[string]interface{}{"type": "string"},
		},
		"required": []string{"action", "asset_type", "value"},
	}
	scope := map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"status":    map[string]interface{}{"type": "string", "enum": []string{"authorized", "out_of_scope", "unknown"}},
			"confirmed": map[string]interface{}{"type": "boolean"}, "action_allowed": map[string]interface{}{"type": "boolean"},
			"scope_revision": map[string]interface{}{"type": "string"}, "source": map[string]interface{}{"type": []string{"string", "null"}},
			"collected_at":  map[string]interface{}{"type": []string{"string", "null"}},
			"matched_rules": map[string]interface{}{"type": "array", "items": matchedRule},
		},
		"required": []string{"status", "confirmed", "action_allowed", "scope_revision", "source", "collected_at", "matched_rules"},
	}
	item := map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"}, "score": map[string]interface{}{"type": "number"},
			"path": map[string]interface{}{"type": "string"}, "source": map[string]interface{}{"type": []string{"string", "null"}},
			"collected_at": map[string]interface{}{"type": []string{"string", "null"}}, "document_type": map[string]interface{}{"type": "string"},
			"effective_scope_status": map[string]interface{}{"type": "string"}, "classification": map[string]interface{}{"type": "string"},
			"untrusted_content": map[string]interface{}{"type": "boolean", "const": true},
		},
		"required": []string{"text", "score", "path", "source", "collected_at", "document_type", "effective_scope_status", "classification", "untrusted_content"},
	}
	return map[string]interface{}{
		"type": "object", "additionalProperties": false,
		"properties": map[string]interface{}{
			"program_id": map[string]interface{}{"type": "string"},
			"asset":      asset,
			"scope":      scope,
			"warnings":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"items":      map[string]interface{}{"type": "array", "items": item},
			"truncated":  map[string]interface{}{"type": "boolean"},
		},
		"required": []string{"program_id", "asset", "scope", "warnings", "items", "truncated"},
	}
}

func sendMCPError(id json.RawMessage, code int, message string) {
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	out, _ := json.Marshal(response)
	fmt.Println(string(out))
}

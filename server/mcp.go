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

func (iw *IngestionWorker) ListenToMCPClient(ctx context.Context) {
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
			iw.handleMCPMethod(req)
		}
	}
}

func (iw *IngestionWorker) handleMCPMethod(req MCPRequest) {
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
		tools := iw.availableTools()
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
			iw.sendMCPError(req.ID, -32602, "Invalid tool call parameters")
			return
		}

		if params.Name == "hive_search" {
			var args HiveSearchArguments
			if err := decodeStrictJSON(params.Arguments, &args); err != nil {
				iw.sendMCPError(req.ID, -32602, "Invalid search arguments format")
				return
			}
			if _, _, err := iw.validateHiveSearch(args); err != nil {
				iw.sendMCPError(req.ID, -32602, err.Error())
				return
			}

			go func() {
				searchResponse, err := iw.HiveSearch(context.Background(), args)
				if err != nil {
					if isSearchValidationError(err) {
						iw.sendMCPError(req.ID, -32602, err.Error())
						return
					}
					log.Printf("Hive search failed: %v", err)
					iw.sendMCPError(req.ID, -32603, "Search execution failed")
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
		} else if params.Name == "get_sync_status" {
			iw.Mu.Lock()
			status := "idle"
			if len(iw.PendingFiles) > 0 || iw.ActiveSyncs > 0 {
				status = "syncing"
			}

			pendingList := []string{}
			for p := range iw.PendingFiles {
				pendingList = append(pendingList, p)
			}

			pendingCount := len(iw.PendingFiles)
			activeCount := iw.ActiveSyncs
			totalCount := iw.TotalSynced
			iw.Mu.Unlock()

			// Format output nicely in Markdown
			var sb strings.Builder
			sb.WriteString("### 🔄 Code Ingestion Sync Status\n\n")
			sb.WriteString(fmt.Sprintf("- **Status:** `%s`\n", status))
			sb.WriteString(fmt.Sprintf("- **Queue Size (Debouncing):** `%d`\n", pendingCount))
			sb.WriteString(fmt.Sprintf("- **Active Indexing Threads:** `%d`\n", activeCount))
			sb.WriteString(fmt.Sprintf("- **Lifetime Synced Files:** `%d`\n", totalCount))

			if len(pendingList) > 0 {
				sb.WriteString("\n#### ⏳ Files Currently in Debounce Queue:\n")
				for _, p := range pendingList {
					sb.WriteString(fmt.Sprintf("- `%s`\n", p))
				}
			}

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
			if !iw.Cfg.IsWriter() {
				iw.sendMCPError(req.ID, -32601, "Requested tool execution target not found")
				return
			}
			go func() {
				count, err := iw.SyncWorkspace(context.Background())
				if err != nil {
					log.Printf("Internal codebase ingestion failed: %v", err)
					iw.sendMCPError(req.ID, -32603, fmt.Sprintf("Ingestion error: %v", err))
					return
				}

				// Respond directly to the active IDE context stream window
				var sb strings.Builder
				sb.WriteString("### 🚀 Codebase Ingestion Complete\n\n")
				sb.WriteString(fmt.Sprintf("Successfully scanned and synchronized **%d** files into the Qdrant collection `%s`.\n", count, iw.Cfg.CollectionName))

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
			}()
		} else {
			iw.sendMCPError(req.ID, -32601, "Requested tool execution target not found")
		}
		return
	}
}

func (iw *IngestionWorker) availableTools() []map[string]interface{} {
	tools := []map[string]interface{}{
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
			"name":        "get_sync_status",
			"description": "Retrieve the local Hive ingestion status.",
			"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
	if iw.Cfg.IsWriter() {
		tools = append(tools, map[string]interface{}{
			"name":        "ingest_workspace",
			"description": "Trigger ingestion from the configured Hive data directory.",
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

// Helper tool to safely write standardized JSON-RPC protocol error contexts
func (iw *IngestionWorker) sendMCPError(id json.RawMessage, code int, message string) {
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

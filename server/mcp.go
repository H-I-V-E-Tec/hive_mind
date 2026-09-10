package server

import (
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

type SearchArguments struct {
	Query          string   `json:"query"`
	ProgramID      string   `json:"program_id"`
	FileExtensions []string `json:"file_extensions,omitempty"`
	PathPrefix     string   `json:"path_prefix,omitempty"`
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

		if params.Name == "qdrant_search" {
			var args SearchArguments
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				iw.sendMCPError(req.ID, -32602, "Invalid search arguments format")
				return
			}

			// Process the RAG search query across the local network interface
			go func() {
				resultsText, err := iw.ExecuteVectorSearch(context.Background(), args.ProgramID, args.Query, args.FileExtensions, args.PathPrefix)
				if err != nil {
					log.Printf("Internal RAG search failed: %v", err)
					iw.sendMCPError(req.ID, -32603, fmt.Sprintf("Search execution error: %v", err))
					return
				}

				// Respond directly to the active IDE context stream window
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]interface{}{
						"content": []map[string]interface{}{
							{
								"type": "text",
								"text": resultsText,
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
			"name":        "qdrant_search",
			"description": "Search the configured Hive collection using semantic vector retrieval.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The semantic search query.",
					},
					"program_id": map[string]interface{}{
						"type":        "string",
						"description": "Program identifier used as a mandatory isolation boundary.",
					},
					"file_extensions": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"path_prefix": map[string]interface{}{"type": "string"},
				},
				"required": []string{"program_id", "query"},
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

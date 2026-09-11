package main

import (
	"qdrant-mcp-server/server"
)

// Version is the current version of the MCP server, injected during the build.
var Version = "1.0.0"
var SourceRevision = "development"

func main() {
	server.SourceRevision = SourceRevision
	server.Start(Version)
}

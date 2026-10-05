package server

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRemoteMCPStartsWithoutLocalConfiguration(t *testing.T) {
	if os.Getenv("HIVE_TEST_REMOTE_MCP") == "1" {
		os.Args = []string{"hive"}
		Start("test")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRemoteMCPStartsWithoutLocalConfiguration$")
	env := []string{"HIVE_TEST_REMOTE_MCP=1", "HIVE_MIND_URL=http://127.0.0.1:1", "HOME=" + t.TempDir(), "USERPROFILE=" + t.TempDir()}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "SYSTEMROOT=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n"))
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		lines <- line
	}()
	select {
	case line := <-lines:
		var resp struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil || resp.JSONRPC != "2.0" || resp.ID != 1 || len(resp.Result) == 0 {
			t.Fatalf("first stdout line is not an initialize response: %q (%v)", line, err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("remote MCP did not answer initialize")
	}
}

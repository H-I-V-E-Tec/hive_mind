package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type RemoteClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRemoteClient(baseURL string) *RemoteClient {
	return &RemoteClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (rc *RemoteClient) HiveSearch(ctx context.Context, args HiveSearchArguments) (HiveSearchResponse, error) {
	var result HiveSearchResponse
	if err := rc.postJSON(ctx, "/api/v1/search", args, &result); err != nil {
		return HiveSearchResponse{}, err
	}
	return result, nil
}

func (rc *RemoteClient) HiveGetContext(ctx context.Context, args HiveContextArguments) (HiveContextResponse, error) {
	var result HiveContextResponse
	if err := rc.postJSON(ctx, "/api/v1/context", args, &result); err != nil {
		return HiveContextResponse{}, err
	}
	return result, nil
}

func (rc *RemoteClient) HiveListTargets(ctx context.Context, args HiveListTargetsArguments) (HiveListTargetsResponse, error) {
	var result HiveListTargetsResponse
	if err := rc.postJSON(ctx, "/api/v1/targets", args, &result); err != nil {
		return HiveListTargetsResponse{}, err
	}
	return result, nil
}

func (rc *RemoteClient) IngestWorkspaceReport(ctx context.Context, _ bool) IngestionReport {
	var result IngestionReport
	if err := rc.postJSON(ctx, "/api/v1/ingest", struct{}{}, &result); err != nil {
		return IngestionReport{OK: false, ExitCode: 1, Error: err.Error()}
	}
	return result
}

func (rc *RemoteClient) SyncStatus() SyncStatusSnapshot {
	var result SyncStatusSnapshot
	if err := rc.getJSON(context.Background(), "/api/v1/sync-status", &result); err != nil {
		return SyncStatusSnapshot{Status: "unknown"}
	}
	return result
}

func (rc *RemoteClient) IsWriter() bool {
	return false
}

func (rc *RemoteClient) Close() {}

func (rc *RemoteClient) postJSON(ctx context.Context, path string, body any, result any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", rc.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return rc.doRequest(req, result)
}

func (rc *RemoteClient) getJSON(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", rc.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return rc.doRequest(req, result)
}

func (rc *RemoteClient) doRequest(req *http.Request, result any) error {
	token, err := LoadStoredToken()
	if err != nil {
		return fmt.Errorf("authentication required: run 'hive login' first (%w)", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("token rejected by server; run 'hive login' to reauthenticate")
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("forbidden: insufficient permissions")
	}
	if resp.StatusCode == http.StatusBadRequest {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &searchValidationError{message: errResp.Error}
		}
		return &searchValidationError{message: "invalid request"}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func programRequest(srv *HTTPServer, token, method, path string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestProgramAccessDeniesOtherProgramsAndLegacyClaims(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	readOnly := memberToken(t, srv, key, kid, []string{permissionMindRead}, jwt.MapClaims{
		"programs": []string{"lab"}, "program_contribute": []string{},
	})
	adminReadOnly := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionScopeAdmin}, jwt.MapClaims{
		"programs": []string{"lab"}, "program_contribute": []string{},
	})
	if rec := programRequest(srv, readOnly, http.MethodGet, "/api/v1/scopes/lab/status", nil); rec.Code != http.StatusOK {
		t.Fatalf("granted scope reader got %d: %s", rec.Code, rec.Body.String())
	}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/search", HiveSearchArguments{ProgramID: "beta", Query: "synthetic"}},
		{http.MethodPost, "/api/v1/context", HiveContextArguments{ProgramID: "beta", Question: "synthetic?", Asset: ContextAsset{Type: "host", Value: "api.example.test"}}},
		{http.MethodPost, "/api/v1/targets", HiveListTargetsArguments{ProgramID: "beta"}},
		{http.MethodGet, "/api/v1/scopes/beta/status", nil},
		{http.MethodPost, "/api/v1/documents/ingest", remoteNote()},
	} {
		if rec := programRequest(srv, readOnly, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
			t.Fatalf("unauthorized program path %s returned %d: %s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if rec := programRequest(srv, adminReadOnly, http.MethodGet, "/api/v1/scopes/lab/approval-preview", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("scope administrator without contribution got %d", rec.Code)
	}
	legacy := memberToken(t, srv, key, kid, []string{permissionMindRead}, jwt.MapClaims{
		"program_access_version": nil, "programs": []string{"lab"}, "program_contribute": []string{},
	})
	if rec := programRequest(srv, legacy, http.MethodGet, "/api/v1/scopes/lab/status", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("legacy token got program access: %d", rec.Code)
	}
	malformed := memberToken(t, srv, key, kid, []string{permissionMindRead}, jwt.MapClaims{
		"programs": []string{"lab", "lab"}, "program_contribute": []string{},
	})
	if rec := programRequest(srv, malformed, http.MethodGet, "/api/v1/scopes/lab/status", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("duplicate program claim got access: %d", rec.Code)
	}
}

func TestProgramAccessGlobalCatalogWithNoGrantsIsEmpty(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := memberToken(t, srv, key, kid, []string{permissionMindRead}, jwt.MapClaims{
		"programs": []string{}, "program_contribute": []string{},
	})
	rec := programRequest(srv, token, http.MethodPost, "/api/v1/targets", HiveListTargetsArguments{})
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-grant catalog got %d: %s", rec.Code, rec.Body.String())
	}
	var result HiveListTargetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result.Targets) != 0 ||
		result.CandidatesEvaluated != 0 || result.UnregisteredFiles != 0 {
		t.Fatalf("empty-grant catalog leaked data: %+v %v", result, err)
	}
}

func TestProgramReaderDoesNotReceiveGlobalSyncCounts(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	srv.worker.Mu.Lock()
	srv.worker.TotalSynced = 42
	srv.worker.Mu.Unlock()
	reader := memberToken(t, srv, key, kid, []string{permissionMindRead}, nil)
	rec := programRequest(srv, reader, http.MethodGet, "/api/v1/sync-status", nil)
	var status SyncStatusSnapshot
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil ||
		status.Status != "available" || status.TotalSynced != 0 || status.PendingFiles != 0 {
		t.Fatalf("reader received global sync counts: %d %+v", rec.Code, status)
	}
	admin := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionScopeAdmin}, nil)
	rec = programRequest(srv, admin, http.MethodGet, "/api/v1/sync-status", nil)
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.TotalSynced != 42 {
		t.Fatalf("administrator lost sync counts: %d %+v", rec.Code, status)
	}
}

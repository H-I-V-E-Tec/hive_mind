package server

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testHTTPServer(t *testing.T) (*HTTPServer, *rsa.PrivateKey, string) {
	t.Helper()
	key := testRSAKey(t)
	kid := "http-test-key"
	jwksSrv := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))

	memory := newMemoryQdrant()
	worker, _ := specWorker(t, memory)
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}

	jwks := NewJWKSClient(jwksSrv.URL)
	httpSrv := NewHTTPServer(worker, jwks, ":0")
	return httpSrv, key, kid
}

func validBearerToken(t *testing.T, key *rsa.PrivateKey, kid, issuer string) string {
	t.Helper()
	return signTestToken(t, key, kid, jwt.MapClaims{
		"iss": issuer, "sub": "member-1",
		"aud": "mind", "exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		"iat": jwt.NewNumericDate(time.Now()), "name": "Ana",
		"permissions": []string{"product.mind"}, "profiles": []string{"membro"},
		"programs": []string{},
	})
}

func TestHealthzNoAuth(t *testing.T) {
	srv, _, _ := testHTTPServer(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("healthz status field = %v", body["status"])
	}
}

func TestSearchWithoutAuth(t *testing.T) {
	srv, _, _ := testHTTPServer(t)
	body, _ := json.Marshal(map[string]any{"program_id": "acme", "query": "test"})
	req := httptest.NewRequest("POST", "/api/v1/search", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("search without auth status = %d, want 401", rec.Code)
	}
}

func TestSearchWithInvalidToken(t *testing.T) {
	srv, _, _ := testHTTPServer(t)
	body, _ := json.Marshal(map[string]any{"program_id": "acme", "query": "test"})
	req := httptest.NewRequest("POST", "/api/v1/search", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("search with invalid token status = %d, want 401", rec.Code)
	}
}

func TestSearchBadBody(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := validBearerToken(t, key, kid, srv.jwks.centerURL)

	req := httptest.NewRequest("POST", "/api/v1/search", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("search bad body status = %d, want 400", rec.Code)
	}
}

func TestSyncStatusWithAuth(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := validBearerToken(t, key, kid, srv.jwks.centerURL)

	req := httptest.NewRequest("GET", "/api/v1/sync-status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sync-status status = %d, want 200", rec.Code)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if body["status"] != "idle" {
		t.Errorf("sync status = %v, want idle", body["status"])
	}
}

func TestIngestRequiresWriter(t *testing.T) {
	key := testRSAKey(t)
	kid := "http-test-key"
	jwksSrv := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))

	// Create a writer worker first to set up infrastructure, then build a reader.
	memory := newMemoryQdrant()
	writerWorker, _ := specWorker(t, memory)
	if err := writerWorker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}

	readerWorker, _ := specWorkerAs(t, memory, "reader-1", "", RoleReader)

	jwks := NewJWKSClient(jwksSrv.URL)
	httpSrv := NewHTTPServer(readerWorker, jwks, ":0")

	token := validBearerToken(t, key, kid, jwksSrv.URL)
	req := httptest.NewRequest("POST", "/api/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	httpSrv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ingest on reader status = %d, want 403", rec.Code)
	}
}

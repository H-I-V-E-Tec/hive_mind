package server

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestReadyzChecksDependenciesWithoutChangingHealthz(t *testing.T) {
	srv, _, _ := testHTTPServer(t)
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := request("/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz status %d: %s", rec.Code, rec.Body.String())
	}
	q := srv.worker.QdrantClient.(*memoryQdrant)
	q.mu.Lock()
	q.collections[srv.worker.Cfg.CollectionName] = false
	q.mu.Unlock()
	if rec := request("/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with missing collection status %d", rec.Code)
	}
	if rec := request("/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz with missing collection status %d", rec.Code)
	}
	q.mu.Lock()
	q.collections[srv.worker.Cfg.CollectionName] = true
	q.mu.Unlock()

	originalTransport := srv.worker.HTTPClient.Transport
	srv.worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
	})
	if rec := request("/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with missing embedding model status %d", rec.Code)
	}
	srv.worker.HTTPClient.Transport = originalTransport

	srv.jwks.mu.Lock()
	srv.jwks.fetched = time.Now().Add(-jwksReadinessTTL - time.Second)
	srv.jwks.mu.Unlock()
	srv.jwks.httpClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	if rec := request("/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with unavailable Center status %d", rec.Code)
	}
}

func TestAuthenticatedRoutesRejectWhenConcurrencyLimitReached(t *testing.T) {
	key := testRSAKey(t)
	kid := "concurrency-test"
	releaseJWKS := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(releaseJWKS) }) }
	jwksJSON := testJWKS(t, kid, &key.PublicKey)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-releaseJWKS
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwksJSON))
	}))
	t.Cleanup(func() {
		unblock()
		jwks.Close()
	})
	srv := NewHTTPServer(nil, NewJWKSClient(jwks.URL), ":0")
	token := validBearerToken(t, key, kid, jwks.URL)
	var requests sync.WaitGroup
	defer func() {
		unblock()
		requests.Wait()
	}()
	for range maxConcurrentMindRequests {
		requests.Add(1)
		go func() {
			defer requests.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/search", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("released request status %d, want 400", rec.Code)
			}
		}()
	}
	deadline := time.After(2 * time.Second)
	for len(srv.requestSlots) != maxConcurrentMindRequests {
		select {
		case <-deadline:
			t.Fatal("requests did not occupy the HTTP concurrency slots")
		case <-time.After(time.Millisecond):
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/search", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response status=%d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	health := httptest.NewRecorder()
	srv.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("healthz should bypass concurrency limit, got %d", health.Code)
	}
	unblock()
	requests.Wait()
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

func TestReadRoutesRejectOversizedBodies(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := validBearerToken(t, key, kid, srv.jwks.centerURL)
	body := append([]byte(`{"program_id":"acme","query":"test"}`), bytes.Repeat([]byte(" "), maxReadRequestBytes)...)
	for _, route := range []string{"/api/v1/search", "/api/v1/context", "/api/v1/targets"} {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status %d, want 413: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestReadRouteRejectsTrailingJSON(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := validBearerToken(t, key, kid, srv.jwks.centerURL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/targets", bytes.NewBufferString(`{"program_id":"acme"}{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
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
	if body["status"] != "available" || body["total_synced"] != float64(0) {
		t.Errorf("reader received global sync details: %+v", body)
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

	token := memberToken(t, httpSrv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)
	body, _ := json.Marshal(remoteNote())
	req := httptest.NewRequest("POST", "/api/v1/documents/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	httpSrv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ingest on reader status = %d, want 403", rec.Code)
	}
}

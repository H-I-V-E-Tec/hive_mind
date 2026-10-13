package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testJWKS(t *testing.T, kid string, pub *rsa.PublicKey) string {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
			"n": n, "e": e,
		}},
	}
	data, _ := json.Marshal(jwks)
	return string(data)
}

func signTestToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func testJWKSServer(t *testing.T, jwksJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/jwks.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, jwksJSON)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestValidateTokenSuccess(t *testing.T) {
	key := testRSAKey(t)
	kid := "test-key-1"
	srv := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))

	jwksClient := NewJWKSClient(srv.URL)
	token := signTestToken(t, key, kid, jwt.MapClaims{
		"iss": srv.URL, "sub": "member-1",
		"aud": "mind", "exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		"iat": jwt.NewNumericDate(time.Now()), "name": "Ana",
		"permissions": []string{"product.mind"}, "profiles": []string{"membro"},
		"programs": []string{},
	})

	claims, err := jwksClient.ValidateToken(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Sub != "member-1" {
		t.Errorf("sub = %q, want member-1", claims.Sub)
	}
	if claims.Name != "Ana" {
		t.Errorf("name = %q, want Ana", claims.Name)
	}
	if claims.Audience != "mind" {
		t.Errorf("audience = %q, want mind", claims.Audience)
	}
	if len(claims.Permissions) != 1 || claims.Permissions[0] != "product.mind" {
		t.Errorf("permissions = %v", claims.Permissions)
	}
}

func TestValidateTokenExpired(t *testing.T) {
	key := testRSAKey(t)
	kid := "test-key-1"
	srv := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))

	jwksClient := NewJWKSClient(srv.URL)
	token := signTestToken(t, key, kid, jwt.MapClaims{
		"iss": srv.URL, "sub": "member-1",
		"aud": "mind", "exp": jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now().Add(-16 * time.Minute)),
		"name": "Ana", "permissions": []string{}, "profiles": []string{}, "programs": []string{},
	})

	_, err := jwksClient.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateTokenWrongAudience(t *testing.T) {
	key := testRSAKey(t)
	kid := "test-key-1"
	srv := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))

	jwksClient := NewJWKSClient(srv.URL)
	token := signTestToken(t, key, kid, jwt.MapClaims{
		"iss": srv.URL, "sub": "member-1",
		"aud": "atlas", "exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now()),
		"name": "Ana", "permissions": []string{}, "profiles": []string{}, "programs": []string{},
	})

	_, err := jwksClient.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateTokenUnknownKid(t *testing.T) {
	key := testRSAKey(t)
	srv := testJWKSServer(t, testJWKS(t, "key-a", &key.PublicKey))

	jwksClient := NewJWKSClient(srv.URL)
	token := signTestToken(t, key, "key-b", jwt.MapClaims{
		"iss": srv.URL, "sub": "member-1",
		"aud": "mind", "exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now()),
		"name": "Ana", "permissions": []string{}, "profiles": []string{}, "programs": []string{},
	})

	_, err := jwksClient.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestJWKSCacheHit(t *testing.T) {
	key := testRSAKey(t)
	kid := "test-key-1"
	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testJWKS(t, kid, &key.PublicKey))
	}))
	t.Cleanup(srv.Close)

	jwksClient := NewJWKSClient(srv.URL)

	for i := 0; i < 3; i++ {
		token := signTestToken(t, key, kid, jwt.MapClaims{
			"iss": srv.URL, "sub": "member-1",
			"aud": "mind", "exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			"iat":  jwt.NewNumericDate(time.Now()),
			"name": "Ana", "permissions": []string{}, "profiles": []string{}, "programs": []string{},
		})
		if _, err := jwksClient.ValidateToken(token); err != nil {
			t.Fatalf("validation %d failed: %v", i, err)
		}
	}

	if fetchCount != 1 {
		t.Errorf("JWKS fetched %d times, want 1 (cache should work)", fetchCount)
	}
}

func TestJWKSUnknownKeyFetchIsBoundedAndRefreshesAfterRetry(t *testing.T) {
	oldKey, newKey := testRSAKey(t), testRSAKey(t)
	var requests atomic.Int32
	var current atomic.Value
	current.Store(testJWKS(t, "old-key", &oldKey.PublicKey))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, current.Load().(string))
	}))
	t.Cleanup(srv.Close)
	client := NewJWKSClient(srv.URL)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.getKey("new-key"); err == nil {
				t.Error("unknown key was accepted")
			}
		}()
	}
	wg.Wait()
	if got := requests.Load(); got != 1 {
		t.Fatalf("concurrent unknown keys triggered %d JWKS requests, want 1", got)
	}

	current.Store(testJWKS(t, "new-key", &newKey.PublicKey))
	client.mu.Lock()
	client.fetched = time.Now().Add(-jwksUnknownKeyRetry - time.Second)
	client.mu.Unlock()
	if _, err := client.getKey("new-key"); err != nil {
		t.Fatalf("rotated key was not loaded: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("rotation triggered %d JWKS requests, want 2", got)
	}
}

func TestJWKSFailedFetchIsBackedOff(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	client := NewJWKSClient(srv.URL)
	for range 5 {
		if _, err := client.getKey("missing-key"); err == nil {
			t.Fatal("failed JWKS fetch was accepted")
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("failed JWKS endpoint was called %d times, want 1", got)
	}
}

func TestJWKSReadinessRespectsDeadlineDuringRefresh(t *testing.T) {
	client := NewJWKSClient("http://center.example.test")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	defer func() {
		close(release)
		<-done
	}()
	client.httpClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return nil, context.Canceled
	})
	go func() {
		client.getKey("missing-key")
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("JWKS refresh did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.ensureFresh(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readiness wait returned %v, want deadline exceeded", err)
	}
}

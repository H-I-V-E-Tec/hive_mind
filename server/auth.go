package server

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	jwksCacheTTL     = 5 * time.Minute
	jwksFetchTimeout = 10 * time.Second
	expectedAudience = "hive"
)

type HiveClaims struct {
	Sub         string   `json:"sub"`
	Name        string   `json:"name"`
	Audience    string   `json:"aud"`
	Issuer      string   `json:"iss"`
	Permissions []string `json:"permissions"`
	Profiles    []string `json:"profiles"`
	Programs    []string `json:"programs"`
}

type jwkEntry struct {
	KID string
	Key *rsa.PublicKey
}

type JWKSClient struct {
	centerURL  string
	httpClient *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func NewJWKSClient(centerURL string) *JWKSClient {
	return &JWKSClient{
		centerURL:  centerURL,
		httpClient: &http.Client{Timeout: jwksFetchTimeout},
		keys:       make(map[string]*rsa.PublicKey),
	}
}

func (j *JWKSClient) ValidateToken(tokenString string) (*HiveClaims, error) {
	token, err := jwt.Parse(tokenString, j.keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(strings.TrimRight(j.centerURL, "/")),
	)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("unexpected claims type")
	}

	aud := claimString(mapClaims, "aud")
	if !supportedAudience(aud) {
		return nil, fmt.Errorf("unexpected audience %q", aud)
	}
	if !validMemberSubject(claimString(mapClaims, "sub")) {
		return nil, errors.New("missing or invalid member subject")
	}

	return &HiveClaims{
		Sub:         claimString(mapClaims, "sub"),
		Name:        claimString(mapClaims, "name"),
		Audience:    aud,
		Issuer:      claimString(mapClaims, "iss"),
		Permissions: claimStringSlice(mapClaims, "permissions"),
		Profiles:    claimStringSlice(mapClaims, "profiles"),
		Programs:    claimStringSlice(mapClaims, "programs"),
	}, nil
}

func (j *JWKSClient) keyFunc(token *jwt.Token) (any, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("token header missing kid")
	}

	key, err := j.getKey(kid)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (j *JWKSClient) getKey(kid string) (*rsa.PublicKey, error) {
	j.mu.RLock()
	if key, ok := j.keys[kid]; ok && time.Since(j.fetched) < jwksCacheTTL {
		j.mu.RUnlock()
		return key, nil
	}
	j.mu.RUnlock()

	if err := j.refresh(); err != nil {
		return nil, fmt.Errorf("JWKS fetch failed: %w", err)
	}

	j.mu.RLock()
	defer j.mu.RUnlock()
	key, ok := j.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	return key, nil
}

func (j *JWKSClient) refresh() error {
	resp, err := j.httpClient.Get(j.centerURL + "/.well-known/jwks.json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("JWKS decode failed: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, raw := range jwks.Keys {
		var entry struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if entry.Kty != "RSA" || entry.Kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(entry.N, entry.E)
		if err != nil {
			continue
		}
		keys[entry.Kid] = pub
	}

	j.mu.Lock()
	j.keys = keys
	j.fetched = time.Now()
	j.mu.Unlock()
	return nil
}

func parseRSAPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() {
		return nil, errors.New("RSA exponent too large")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

func claimString(claims jwt.MapClaims, key string) string {
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

func claimStringSlice(claims jwt.MapClaims, key string) []string {
	arr, ok := claims[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// Legacy Mind sessions remain valid until expiry during the shared-login rollout.
func supportedAudience(aud string) bool { return aud == expectedAudience || aud == "mind" }

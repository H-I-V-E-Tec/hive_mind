package server

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	jwksCacheTTL        = 5 * time.Minute
	jwksFetchTimeout    = 10 * time.Second
	jwksReadinessTTL    = 15 * time.Second
	jwksUnknownKeyRetry = 10 * time.Second
	jwksErrorRetry      = 5 * time.Second
	jwksMaxBodyBytes    = 1 << 20
	expectedAudience    = "hive"
)

type HiveClaims struct {
	Sub                  string   `json:"sub"`
	Name                 string   `json:"name"`
	Audience             string   `json:"aud"`
	Issuer               string   `json:"iss"`
	Permissions          []string `json:"permissions"`
	Profiles             []string `json:"profiles"`
	Programs             []string `json:"programs"`
	ProgramAccessVersion int      `json:"program_access_version"`
	ProgramContribute    []string `json:"program_contribute"`
}

type jwkEntry struct {
	KID string
	Key *rsa.PublicKey
}

type JWKSClient struct {
	centerURL  string
	httpClient *http.Client

	mu          sync.RWMutex
	refreshSlot chan struct{}
	keys        map[string]*rsa.PublicKey
	fetched     time.Time
	lastAttempt time.Time
	lastError   error
}

func NewJWKSClient(centerURL string) *JWKSClient {
	return &JWKSClient{
		centerURL:   centerURL,
		httpClient:  &http.Client{Timeout: jwksFetchTimeout},
		keys:        make(map[string]*rsa.PublicKey),
		refreshSlot: make(chan struct{}, 1),
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

	programs, readOK := strictProgramList(mapClaims["programs"])
	contribute, contributeOK := strictProgramList(mapClaims["program_contribute"])
	version := 0
	if mapClaims["program_access_version"] == float64(1) && readOK && contributeOK && validProgramGrants(programs, contribute) {
		version = 1
	}
	return &HiveClaims{
		Sub:         claimString(mapClaims, "sub"),
		Name:        claimString(mapClaims, "name"),
		Audience:    aud,
		Issuer:      claimString(mapClaims, "iss"),
		Permissions: claimStringSlice(mapClaims, "permissions"),
		Profiles:    claimStringSlice(mapClaims, "profiles"),
		Programs:    programs, ProgramContribute: contribute, ProgramAccessVersion: version,
	}, nil
}

func strictProgramList(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok || len(items) > 128 {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		program, ok := item.(string)
		if !ok || !validIdentifier(program, 64) {
			return nil, false
		}
		result = append(result, program)
	}
	return result, true
}

func validProgramGrants(read, contribute []string) bool {
	if read == nil || contribute == nil || len(read) > 128 || len(contribute) > 128 {
		return false
	}
	for index, program := range read {
		if !validIdentifier(program, 64) || index > 0 && read[index-1] >= program {
			return false
		}
	}
	for index, program := range contribute {
		if !validIdentifier(program, 64) || index > 0 && contribute[index-1] >= program {
			return false
		}
		at := sort.SearchStrings(read, program)
		if at >= len(read) || read[at] != program {
			return false
		}
	}
	return true
}

func (claims *HiveClaims) CanReadProgram(program string) bool {
	if claims == nil || claims.ProgramAccessVersion != 1 || !validProgramGrants(claims.Programs, claims.ProgramContribute) {
		return false
	}
	at := sort.SearchStrings(claims.Programs, program)
	return at < len(claims.Programs) && claims.Programs[at] == program
}

func (claims *HiveClaims) CanContributeProgram(program string) bool {
	if !claims.CanReadProgram(program) {
		return false
	}
	at := sort.SearchStrings(claims.ProgramContribute, program)
	return at < len(claims.ProgramContribute) && claims.ProgramContribute[at] == program
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
	key, refresh, err := j.cachedKey(kid)
	if !refresh {
		return key, err
	}
	j.refreshSlot <- struct{}{}
	defer func() { <-j.refreshSlot }()
	key, refresh, err = j.cachedKey(kid)
	if !refresh {
		return key, err
	}
	if err := j.refresh(context.Background()); err != nil {
		return nil, fmt.Errorf("JWKS fetch failed: %w", err)
	}
	key, _, err = j.cachedKey(kid)
	return key, err
}

func (j *JWKSClient) cachedKey(kid string) (*rsa.PublicKey, bool, error) {
	j.mu.RLock()
	key, ok := j.keys[kid]
	fetched, attempted, lastError := j.fetched, j.lastAttempt, j.lastError
	j.mu.RUnlock()
	if ok && time.Since(fetched) < jwksCacheTTL {
		return key, false, nil
	}
	if !ok && !fetched.IsZero() && time.Since(fetched) < jwksUnknownKeyRetry {
		return nil, false, fmt.Errorf("key %q not found in JWKS", kid)
	}
	if lastError != nil && time.Since(attempted) < jwksErrorRetry {
		return nil, false, fmt.Errorf("JWKS fetch failed: %w", lastError)
	}
	return nil, true, nil
}

// ensureFresh checks that the signing keys are available for readiness probes.
func (j *JWKSClient) ensureFresh(ctx context.Context) error {
	j.mu.RLock()
	fresh := len(j.keys) > 0 && time.Since(j.fetched) < jwksReadinessTTL
	j.mu.RUnlock()
	if fresh {
		return nil
	}
	select {
	case j.refreshSlot <- struct{}{}:
		defer func() { <-j.refreshSlot }()
	case <-ctx.Done():
		return ctx.Err()
	}
	j.mu.RLock()
	fresh = len(j.keys) > 0 && time.Since(j.fetched) < jwksReadinessTTL
	attempted, lastError := j.lastAttempt, j.lastError
	j.mu.RUnlock()
	if fresh {
		return nil
	}
	if lastError != nil && time.Since(attempted) < jwksErrorRetry {
		return lastError
	}
	return j.refresh(ctx)
}

// refresh is serialized by refreshSlot; failed fetches are retried after a short delay.
func (j *JWKSClient) refresh(ctx context.Context) (resultErr error) {
	j.mu.Lock()
	j.lastAttempt = time.Now()
	j.mu.Unlock()
	defer func() {
		j.mu.Lock()
		j.lastError = resultErr
		j.mu.Unlock()
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.centerURL+"/.well-known/jwks.json", nil)
	if err != nil {
		return err
	}
	resp, err := j.httpClient.Do(req)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("JWKS read failed: %w", err)
	}
	if len(body) > jwksMaxBodyBytes {
		return errors.New("JWKS response exceeds the size limit")
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
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
	if len(keys) == 0 {
		return errors.New("JWKS contains no supported signing keys")
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

package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const tokenDirPerm = 0o700
const tokenFilePerm = 0o600

func TokenDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hive")
}

func TokenPath() string {
	dir := TokenDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "token")
}

func LoadStoredToken() (string, error) {
	path := TokenPath()
	if path == "" {
		return "", errors.New("cannot determine token path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no stored token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("stored token is empty")
	}
	return token, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type tokenErrorResponse struct {
	Error string   `json:"error"`
	Valid []string `json:"valid,omitempty"`
}

func RunLogin(centerURL string, stdin io.Reader, stderr io.Writer) error {
	centerURL, err := ValidateHiveCenterURL(centerURL)
	if err != nil {
		return err
	}

	fmt.Fprint(stderr, "Username: ")
	var username string
	if _, err := fmt.Fscanln(stdin, &username); err != nil {
		return errors.New("failed to read username")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username cannot be empty")
	}

	fmt.Fprint(stderr, "Password: ")
	var password string
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return errors.New("failed to read password")
		}
		password = string(raw)
	} else {
		if _, err := fmt.Fscanln(stdin, &password); err != nil {
			return errors.New("failed to read password")
		}
	}
	if password == "" {
		return errors.New("password cannot be empty")
	}

	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
		"audience": "mind",
	})

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Post(
		centerURL+"/auth/token",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("cannot reach HIVE Center: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return errors.New("failed to read response")
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("login failed: %s", errResp.Error)
		}
		return fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return errors.New("unexpected response format")
	}
	if tokenResp.AccessToken == "" {
		return errors.New("server returned empty token")
	}

	if err := storeToken(tokenResp.AccessToken); err != nil {
		return err
	}

	name := tokenDisplayName(tokenResp.AccessToken)
	if name == "" {
		name = username
	}
	fmt.Fprintf(stderr, "Authenticated as %s (token expires in %d seconds)\n", name, tokenResp.ExpiresIn)
	fmt.Fprintf(stderr, "Token saved to %s\n", TokenPath())
	return nil
}

func RunLoginCheck(stderr io.Writer) error {
	token, err := LoadStoredToken()
	if err != nil {
		return err
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("stored token has invalid format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("stored token payload is not valid base64")
	}

	var claims struct {
		Sub  string `json:"sub"`
		Name string `json:"name"`
		Exp  int64  `json:"exp"`
		Aud  string `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return errors.New("stored token payload is not valid JSON")
	}

	expiry := time.Unix(claims.Exp, 0)
	if time.Now().After(expiry) {
		fmt.Fprintf(stderr, "Token expired at %s (user: %s)\n", expiry.Format(time.RFC3339), claims.Name)
		return errors.New("token is expired; run login again")
	}

	fmt.Fprintf(stderr, "Token valid for %s (user: %s, audience: %s, expires: %s)\n",
		claims.Name, claims.Sub, claims.Aud, expiry.Format(time.RFC3339))
	return nil
}

func storeToken(token string) error {
	dir := TokenDir()
	if dir == "" {
		return errors.New("cannot determine home directory")
	}
	if err := os.MkdirAll(dir, tokenDirPerm); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(token+"\n"), tokenFilePerm); err != nil {
		return fmt.Errorf("cannot write token: %w", err)
	}
	return nil
}

func tokenDisplayName(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Name
}

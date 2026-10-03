package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DoctorReport struct {
	OK       bool          `json:"ok"`
	Mode     string        `json:"mode"`
	Version  string        `json:"version"`
	Platform string        `json:"platform"`
	Checks   []DoctorCheck `json:"checks"`
}

func RunDoctor(stderr io.Writer) DoctorReport {
	report := DoctorReport{
		OK:       true,
		Version:  Version,
		Platform: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	mindURL := os.Getenv("HIVE_MIND_URL")
	if mindURL != "" {
		report.Mode = "remote"
		runRemoteDoctor(&report, mindURL, stderr)
	} else {
		report.Mode = "local"
		runLocalDoctor(&report, stderr)
	}

	return report
}

func runRemoteDoctor(report *DoctorReport, mindURL string, stderr io.Writer) {
	fmt.Fprintf(stderr, "Hive Mind %s (%s) — modo remoto\n\n", Version, report.Platform)

	// 1. Validate HIVE_MIND_URL
	validatedURL, err := ValidateHiveCenterURL(mindURL)
	if err != nil {
		report.addFail("mind_url", fmt.Sprintf("URL inválida: %v", err))
		fmt.Fprintf(stderr, "  ✗ HIVE_MIND_URL: %v\n", err)
		return
	}
	report.addPass("mind_url", validatedURL)
	fmt.Fprintf(stderr, "  ✓ HIVE_MIND_URL: %s\n", validatedURL)

	// 2. Check stored token
	token, err := LoadStoredToken()
	if err != nil {
		report.addFail("token", fmt.Sprintf("sem token: %v", err))
		fmt.Fprintf(stderr, "  ✗ Token: não encontrado — rode 'hive login'\n")
	} else {
		name := tokenDisplayName(token)
		expiry, expired := tokenExpiry(token)
		if expired {
			report.addFail("token", fmt.Sprintf("expirado em %s (usuário: %s)", expiry.Format(time.RFC3339), name))
			fmt.Fprintf(stderr, "  ✗ Token: expirado em %s — rode 'hive login'\n", expiry.Format(time.RFC3339))
		} else {
			remaining := time.Until(expiry).Truncate(time.Second)
			report.addPass("token", fmt.Sprintf("válido para %s (expira em %s)", name, remaining))
			fmt.Fprintf(stderr, "  ✓ Token: válido para %s (expira em %s)\n", name, remaining)
		}
	}

	// 3. Check server connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", validatedURL+"/healthz", nil)
	if err != nil {
		report.addFail("connectivity", fmt.Sprintf("erro ao montar requisição: %v", err))
		fmt.Fprintf(stderr, "  ✗ Servidor: erro na requisição\n")
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		report.addFail("connectivity", fmt.Sprintf("não alcançável: %v", err))
		fmt.Fprintf(stderr, "  ✗ Servidor: não alcançável\n")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		report.addFail("connectivity", fmt.Sprintf("healthz retornou %d", resp.StatusCode))
		fmt.Fprintf(stderr, "  ✗ Servidor: healthz retornou %d\n", resp.StatusCode)
		return
	}

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &health) == nil && health.Status == "ok" {
		detail := fmt.Sprintf("ok (versão %s)", health.Version)
		report.addPass("connectivity", detail)
		fmt.Fprintf(stderr, "  ✓ Servidor: %s\n", detail)
	} else {
		report.addFail("connectivity", "resposta inesperada do healthz")
		fmt.Fprintf(stderr, "  ✗ Servidor: resposta inesperada\n")
	}

	// 4. Check authenticated access
	if token != "" {
		req2, _ := http.NewRequestWithContext(ctx, "GET", validatedURL+"/api/v1/sync-status", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			report.addFail("auth_access", "requisição autenticada falhou")
			fmt.Fprintf(stderr, "  ✗ Acesso autenticado: falhou\n")
		} else {
			resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				report.addPass("auth_access", "autorizado")
				fmt.Fprintf(stderr, "  ✓ Acesso autenticado: autorizado\n")
			} else if resp2.StatusCode == http.StatusUnauthorized {
				report.addFail("auth_access", "token rejeitado pelo servidor")
				fmt.Fprintf(stderr, "  ✗ Acesso autenticado: token rejeitado\n")
			} else {
				report.addFail("auth_access", fmt.Sprintf("status %d", resp2.StatusCode))
				fmt.Fprintf(stderr, "  ✗ Acesso autenticado: status %d\n", resp2.StatusCode)
			}
		}
	}

	fmt.Fprintln(stderr)
	if report.OK {
		fmt.Fprintln(stderr, "Tudo pronto.")
	} else {
		fmt.Fprintln(stderr, "Problemas encontrados — corrija os itens acima.")
	}
}

func runLocalDoctor(report *DoctorReport, stderr io.Writer) {
	fmt.Fprintf(stderr, "Hive Mind %s (%s) — modo local\n\n", Version, report.Platform)

	// Check required env/config
	for _, key := range []string{"QDRANT_URL", "OLLAMA_URL", "HIVE_COLLECTION", "HIVE_ROLE"} {
		val := os.Getenv(key)
		if val != "" {
			report.addPass(strings.ToLower(key), val)
			fmt.Fprintf(stderr, "  ✓ %s: %s\n", key, val)
		} else {
			report.addFail(strings.ToLower(key), "não configurado")
			fmt.Fprintf(stderr, "  ✗ %s: não configurado\n", key)
		}
	}

	fmt.Fprintln(stderr)
	if report.OK {
		fmt.Fprintln(stderr, "Configuração local presente.")
	} else {
		fmt.Fprintln(stderr, "Variáveis obrigatórias ausentes.")
	}
	fmt.Fprintln(stderr, "Dica: configure HIVE_MIND_URL para modo remoto (sem Qdrant/Ollama local).")
}

func tokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, true
	}
	payload, err := decodeJWTPayload(parts[1])
	if err != nil {
		return time.Time{}, true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return time.Time{}, true
	}
	expiry := time.Unix(claims.Exp, 0)
	return expiry, time.Now().After(expiry)
}

func (r *DoctorReport) addPass(name, detail string) {
	r.Checks = append(r.Checks, DoctorCheck{Name: name, Status: "ok", Detail: detail})
}

func (r *DoctorReport) addFail(name, detail string) {
	r.OK = false
	r.Checks = append(r.Checks, DoctorCheck{Name: name, Status: "failed", Detail: detail})
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type contextKey string

const claimsContextKey contextKey = "hiveClaims"

func claimsFromContext(ctx context.Context) *HiveClaims {
	if c, ok := ctx.Value(claimsContextKey).(*HiveClaims); ok {
		return c
	}
	return nil
}

type HTTPServer struct {
	worker *IngestionWorker
	jwks   *JWKSClient
	mux    *http.ServeMux
	server *http.Server
}

func NewHTTPServer(worker *IngestionWorker, jwks *JWKSClient, addr string) *HTTPServer {
	s := &HTTPServer{
		worker: worker,
		jwks:   jwks,
		mux:    http.NewServeMux(),
	}
	s.registerRoutes()
	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

func (s *HTTPServer) registerRoutes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("POST /api/v1/search", s.authMiddleware(http.HandlerFunc(s.handleSearch)))
	s.mux.Handle("POST /api/v1/context", s.authMiddleware(http.HandlerFunc(s.handleContext)))
	s.mux.Handle("POST /api/v1/targets", s.authMiddleware(http.HandlerFunc(s.handleTargets)))
	s.mux.Handle("GET /api/v1/sync-status", s.authMiddleware(http.HandlerFunc(s.handleSyncStatus)))
	s.mux.Handle("POST /api/v1/documents/ingest", s.authMiddleware(s.instanceWriterMiddleware(http.HandlerFunc(s.handleIngestDocument))))
	s.mux.Handle("POST /api/v1/scopes/{programID}/approve", s.authMiddleware(s.scopeAdminMiddleware(http.HandlerFunc(s.handleApproveScope))))
}

func (s *HTTPServer) scopeAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !claimsFromContext(r.Context()).HasPermissions(permissionMindRead, permissionScopeAdmin) {
			writeJSONError(w, http.StatusForbidden, "product.mind and mind.scope.approve permissions are required")
			return
		}
		if !s.worker.Cfg.IsWriter() {
			writeJSONError(w, http.StatusForbidden, "this instance is not a writer")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) ListenAndServe() error {
	return s.server.ListenAndServe()
}

func (s *HTTPServer) ListenAndServeTLS(certFile, keyFile string) error {
	return s.server.ListenAndServeTLS(certFile, keyFile)
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Handler returns the configured mux for testing.
func (s *HTTPServer) Handler() http.Handler {
	return s.mux
}

// --- Middleware ---

func (s *HTTPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSONError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		tokenString := strings.TrimPrefix(auth, "Bearer ")

		claims, err := s.jwks.ValidateToken(tokenString)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if !claims.HasPermissions(permissionMindRead) {
			writeJSONError(w, http.StatusForbidden, "product.mind permission is required")
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *HTTPServer) instanceWriterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.worker.Cfg.IsWriter() {
			writeJSONError(w, http.StatusForbidden, "this instance is not a writer")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Handlers ---

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": Version})
}

func (s *HTTPServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	var args HiveSearchArguments
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, _, err := s.worker.validateHiveSearch(args); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.worker.HiveSearch(r.Context(), args)
	if err != nil {
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) handleContext(w http.ResponseWriter, r *http.Request) {
	var args HiveContextArguments
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, _, _, err := s.worker.validateHiveContext(args); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.worker.HiveGetContext(r.Context(), args)
	if err != nil {
		writeSearchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) handleTargets(w http.ResponseWriter, r *http.Request) {
	var args HiveListTargetsArguments
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, _, err := validateHiveListTargets(args); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.worker.HiveListTargets(r.Context(), args)
	if err != nil {
		log.Printf("target catalog failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "target catalog failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) handleSyncStatus(w http.ResponseWriter, _ *http.Request) {
	s.worker.Mu.Lock()
	status := "idle"
	if len(s.worker.PendingFiles) > 0 || s.worker.ActiveSyncs > 0 {
		status = "syncing"
	}
	pending := len(s.worker.PendingFiles)
	active := s.worker.ActiveSyncs
	total := s.worker.TotalSynced
	s.worker.Mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        status,
		"pending_files": pending,
		"active_syncs":  active,
		"total_synced":  total,
	})
}

func (s *HTTPServer) handleIngestDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, remoteRequestLimit)
	defer r.Body.Close()
	var args HiveIngestDocumentArguments
	body, err := io.ReadAll(r.Body)
	if err == nil {
		if !utf8.Valid(body) {
			err = errors.New("request must be UTF-8")
		} else {
			err = decodeStrictJSON(body, &args)
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body exceeds the limit")
		} else {
			writeJSONError(w, http.StatusBadRequest, "invalid document request; unknown fields and trailing JSON are not allowed")
		}
		return
	}
	if err := validateRemoteDocument(args); err != nil {
		status := http.StatusBadRequest
		if (args.DocumentType == "scope" && len(args.Content) > remoteScopeLimit) ||
			(args.DocumentType != "scope" && len(args.Content) > remoteContentLimit) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSONError(w, status, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), remoteIngestTimeout)
	defer cancel()
	report := s.worker.IngestRemoteDocument(ctx, claimsFromContext(ctx), args)
	status := http.StatusOK
	if !report.OK {
		status = http.StatusInternalServerError
		if report.ExitCode == ExitUsage {
			status = http.StatusBadRequest
		} else if report.ExitCode == ExitAuthorization {
			status = http.StatusForbidden
		}
	}
	writeJSON(w, status, report)
}

func (s *HTTPServer) handleApproveScope(w http.ResponseWriter, r *http.Request) {
	programID := r.PathValue("programID")
	var body struct {
		SHA256 string `json:"sha256"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(raw) > 1024 || decodeStrictJSON(raw, &body) != nil || len(body.SHA256) != 64 {
		writeJSONError(w, http.StatusBadRequest, "a valid sha256 confirmation is required")
		return
	}
	if err := s.worker.ApproveScopeRevision(r.Context(), programID, body.SHA256); err != nil {
		var operational *operationalError
		if errors.As(err, &operational) && operational.code == ExitUsage {
			writeJSONError(w, http.StatusConflict, operational.message)
			return
		}
		log.Printf("scope approval failed for program_id=%s: %v", programID, err)
		writeJSONError(w, http.StatusInternalServerError, "scope approval failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"program_id": programID, "status": "approved", "scope_revision": body.SHA256})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("HTTP response encode failed: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeSearchError(w http.ResponseWriter, err error) {
	if isSearchValidationError(err) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("operation failed: %v", err)
	writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("operation failed"))
}

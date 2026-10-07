package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestSharedHiveTokenUsesMindPermissions(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	for _, tc := range []struct {
		name   string
		grants []string
		status int
	}{
		{"both", []string{"product.mind", "product.atlas"}, 200},
		{"mind", []string{"product.mind"}, 200},
		{"atlas", []string{"product.atlas"}, 403},
		{"none", nil, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := memberToken(t, srv, key, kid, tc.grants, jwt.MapClaims{"aud": "hive"})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-status", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMindClientLoadsLauncherSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HIVE_HOME", root)
	t.Setenv("HIVE_TOKEN", "")
	t.Setenv("HIVE_TOKEN_FILE", "")
	srv, key, kid := testHTTPServer(t)
	token := memberToken(t, srv, key, kid, []string{"product.mind", "product.atlas", "mind.ingest"}, jwt.MapClaims{"aud": "hive"})
	if err := os.WriteFile(TokenPath(), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CenterURLPath(), []byte(srv.jwks.centerURL), 0600); err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	client := NewRemoteClient(httpSrv.URL)
	if !client.CanIngestDocument() {
		t.Fatal("shared JWT did not enable permitted remote ingestion")
	}
	var result SyncStatusSnapshot
	if err := client.getJSON(context.Background(), "/api/v1/sync-status", &result); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(LoadStoredCenterURL()) != srv.jwks.centerURL {
		t.Fatal("did not load shared Center URL")
	}
}

package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type permissionQdrant struct {
	*memoryQdrant
	writeErr error
}

func TestSpec006TLSRejectsWrongHostnameAndExpiredCertificate(t *testing.T) {
	rootCert, rootKey := testCertificateAuthority(t)
	caPath := filepath.Join(t.TempDir(), "hive-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootCert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	validCert := testServerCertificate(t, rootCert, rootKey, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err := verifyTestServerCertificate(validCert, Config{QdrantUseTLS: true, QdrantHost: "localhost", QdrantTLSCAFile: caPath}); err != nil {
		t.Fatalf("valid private CA and hostname were rejected: %v", err)
	}
	if err := verifyTestServerCertificate(validCert, Config{QdrantUseTLS: true, QdrantHost: "wrong.example", QdrantTLSCAFile: caPath}); err == nil {
		t.Fatal("certificate with the wrong hostname was accepted")
	}

	expiredCert := testServerCertificate(t, rootCert, rootKey, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	if err := verifyTestServerCertificate(expiredCert, Config{QdrantUseTLS: true, QdrantHost: "localhost", QdrantTLSCAFile: caPath}); err == nil {
		t.Fatal("expired certificate was accepted")
	}
}

func testCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Hive test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func testServerCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, notBefore, notAfter time.Time) *x509.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(notAfter.UnixNano()), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func verifyTestServerCertificate(certificate *x509.Certificate, cfg Config) error {
	tlsConfig, err := cfg.QdrantTLSConfig()
	if err != nil {
		return err
	}
	_, err = certificate.Verify(x509.VerifyOptions{Roots: tlsConfig.RootCAs, DNSName: tlsConfig.ServerName, CurrentTime: time.Now()})
	return err
}

func TestSpec006ReferenceDeploymentIsPrivateAndUnprivileged(t *testing.T) {
	composeBytes, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	dockerfileBytes, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	runbookBytes, err := os.ReadFile("../docs/operations/qdrant-access.md")
	if err != nil {
		t.Fatal(err)
	}
	compose := string(composeBytes)
	dockerfile := string(dockerfileBytes)
	runbook := string(runbookBytes)
	for _, required := range []string{
		"qdrant/qdrant:v1.10.0", "127.0.0.1:6334:6334",
		"qdrant_storage:/qdrant/storage", "HIVE_QDRANT_ADMIN_KEY", "HIVE_QDRANT_WRITER_TOKEN",
		"read_only: true", "cap_drop:", "no-new-privileges:true", ".:/workspace:ro",
	} {
		if !strings.Contains(compose, required) {
			t.Errorf("reference deployment is missing %q", required)
		}
	}
	if strings.Contains(compose, "HIVE_QDRANT_BIND_IP") || strings.Contains(compose, `- "6334:6334"`) || strings.Contains(compose, `- "0.0.0.0:6334:6334"`) {
		t.Fatal("reference deployment exposes Qdrant on all interfaces")
	}
	if !strings.Contains(dockerfile, "USER hive") {
		t.Fatal("runtime image does not select the unprivileged Hive user")
	}
	for _, section := range []string{"## Admissão de dispositivo", "## Rotação e revogação", "## Revogação de dispositivo"} {
		if !strings.Contains(runbook, section) {
			t.Errorf("Qdrant access runbook is missing %q", section)
		}
	}
}

func (q *permissionQdrant) Upsert(ctx context.Context, in *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	if q.writeErr != nil {
		return nil, q.writeErr
	}
	return q.memoryQdrant.Upsert(ctx, in)
}

func (q *permissionQdrant) Delete(ctx context.Context, in *qdrant.DeletePoints) (*qdrant.UpdateResult, error) {
	if q.writeErr != nil {
		return nil, q.writeErr
	}
	return q.memoryQdrant.Delete(ctx, in)
}

func TestSpec006CredentialCapabilitiesMatchRole(t *testing.T) {
	q := newMemoryQdrant()
	writer, _ := specWorker(t, q)
	if err := writer.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.ValidateCredentialCapabilities(context.Background()); err != nil {
		t.Fatalf("writer read-write capability was rejected: %v", err)
	}

	reader := &IngestionWorker{Cfg: writer.Cfg, QdrantClient: &permissionQdrant{memoryQdrant: q, writeErr: status.Error(codes.PermissionDenied, "synthetic denial")}}
	reader.Cfg.Role = RoleReader
	if err := reader.ValidateCredentialCapabilities(context.Background()); err != nil {
		t.Fatalf("read-only credential was rejected: %v", err)
	}

	reader.QdrantClient = &permissionQdrant{memoryQdrant: q}
	if err := reader.ValidateCredentialCapabilities(context.Background()); err == nil || err.Error() != "Qdrant reader credential permits writes" {
		t.Fatalf("write-capable reader credential was accepted: %v", err)
	}
}

func TestSpec006CredentialFailuresAreFailClosedAndSanitized(t *testing.T) {
	q := newMemoryQdrant()
	q.collections["hive_data"] = true
	q.collections["hive_data__control"] = true
	q.points["hive_data"] = map[string]*qdrant.PointStruct{}
	q.points["hive_data__control"] = map[string]*qdrant.PointStruct{}
	worker := &IngestionWorker{Cfg: Config{HiveID: "test-hive", DeviceID: "reader-1", Role: RoleReader, CollectionName: "hive_data", ControlCollection: "hive_data__control"}}
	secretDetail := "synthetic-secret-must-not-leak"
	worker.QdrantClient = &permissionQdrant{memoryQdrant: q, writeErr: status.Error(codes.Unauthenticated, secretDetail)}
	err := worker.ValidateCredentialCapabilities(context.Background())
	if err == nil || strings.Contains(err.Error(), secretDetail) {
		t.Fatalf("authentication failure was not sanitized: %v", err)
	}
}

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	parserVersion        = "document-parser-v1"
	chunkerVersion       = "document-chunker-v1"
	payloadSchemaVersion = "1"
)

type documentHead struct {
	DocumentID       string
	ProgramID        string
	Path             string
	DocumentRevision string
	ScopeRevision    string
	ChunkCount       int
	State            string
	PendingSince     string
	CreatedAt        string
	WriterDeviceID   string
	Bytes            int64
}

// errForeignDocument marks a document whose head belongs to another writer.
// Writers skip it instead of failing so synchronized folders can be ingested
// from several devices without clobbering each other's revisions.
var errForeignDocument = errors.New("document is owned by another writer")

func (iw *IngestionWorker) ownsHead(head *documentHead) bool {
	return head == nil || head.WriterDeviceID == "" || head.WriterDeviceID == iw.Cfg.DeviceID
}

// headPayload is the single serializer for document_head records so every
// rewrite preserves ownership and size metadata.
func headPayload(head *documentHead, now string) map[string]any {
	payload := map[string]any{
		"document_id": head.DocumentID, "program_id": head.ProgramID, "path": head.Path,
		"active_document_revision": head.DocumentRevision, "active_scope_revision": head.ScopeRevision,
		"chunk_count": int64(head.ChunkCount), "state": head.State, "created_at": head.CreatedAt, "updated_at": now,
		"writer_device_id": head.WriterDeviceID, "document_bytes": head.Bytes,
	}
	if head.PendingSince != "" {
		payload["pending_since"] = head.PendingSince
	}
	return payload
}

type ollamaTagsResponse struct {
	Models []struct {
		Name   string `json:"name"`
		Model  string `json:"model"`
		Digest string `json:"digest"`
	} `json:"models"`
}

// EnsureInfrastructure applies spec-000's writer-only topology before mutation.
func (iw *IngestionWorker) EnsureInfrastructure(ctx context.Context) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("collection reconciliation requires HIVE_ROLE=writer")
	}
	if !validIdentifier(iw.Cfg.WriterApprovalID, 64) {
		return errors.New("writer infrastructure requires a valid HIVE_WRITER_APPROVAL_ID")
	}
	iw.infrastructureMu.Lock()
	defer iw.infrastructureMu.Unlock()
	if iw.infrastructureReady {
		return nil
	}
	if err := iw.audit(AuditEvent{Action: "infrastructure", Outcome: "prepared"}, true); err != nil {
		return err
	}

	dimensionVector, err := iw.FetchRemoteEmbedding(ctx, "hive-mind dimension probe")
	if err != nil {
		return fmt.Errorf("resolve embedding dimension: %w", err)
	}
	if len(dimensionVector) == 0 {
		return errors.New("embedding model returned an empty dimension probe")
	}
	digest, err := iw.fetchEmbeddingModelDigest(ctx)
	if err != nil {
		return err
	}

	dataExists, err := iw.QdrantClient.CollectionExists(ctx, iw.Cfg.CollectionName)
	if err != nil {
		return fmt.Errorf("inspect data collection: %w", err)
	}
	if !dataExists {
		if err := iw.QdrantClient.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName:      iw.Cfg.CollectionName,
			VectorsConfig:       qdrant.NewVectorsConfig(&qdrant.VectorParams{Size: uint64(len(dimensionVector)), Distance: qdrant.Distance_Cosine}),
			SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{"sparse": {}}),
		}); err != nil {
			return fmt.Errorf("create data collection: %w", err)
		}
	} else if err := iw.validateExistingCollection(ctx, iw.Cfg.CollectionName, uint64(len(dimensionVector)), false); err != nil {
		return err
	}
	controlExists, err := iw.QdrantClient.CollectionExists(ctx, iw.Cfg.ControlCollection)
	if err != nil {
		return fmt.Errorf("inspect control collection: %w", err)
	}
	if !controlExists {
		if err := iw.QdrantClient.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: iw.Cfg.ControlCollection,
			VectorsConfig:  qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{}),
		}); err != nil {
			return fmt.Errorf("create control collection: %w", err)
		}
	} else if err := iw.validateExistingCollection(ctx, iw.Cfg.ControlCollection, 0, true); err != nil {
		return err
	}
	if err := iw.ensurePayloadIndexes(ctx); err != nil {
		return err
	}

	manifest := iw.expectedManifest(len(dimensionVector), digest)
	if err := iw.ensureImmutableControl(ctx, "collection_manifest", iw.Cfg.HiveID, manifest); err != nil {
		return fmt.Errorf("collection manifest: %w", err)
	}
	registration := map[string]any{
		"record_type": "writer_registration", "hive_id": iw.Cfg.HiveID,
		"writer_device_id": iw.Cfg.DeviceID, "operational_approval_id": iw.Cfg.WriterApprovalID,
	}
	if err := iw.ensureImmutableControl(ctx, "writer_registration", iw.Cfg.DeviceID, registration); err != nil {
		return fmt.Errorf("writer registration: %w", err)
	}
	iw.infrastructureReady = true
	return nil
}

func (iw *IngestionWorker) ensurePayloadIndexes(ctx context.Context) error {
	dataIndexes := map[string]qdrant.FieldType{
		"hive_id": qdrant.FieldType_FieldTypeKeyword, "program_id": qdrant.FieldType_FieldTypeKeyword,
		"record_type": qdrant.FieldType_FieldTypeKeyword, "document_type": qdrant.FieldType_FieldTypeKeyword,
		"claimed_scope_status": qdrant.FieldType_FieldTypeKeyword, "effective_scope_status": qdrant.FieldType_FieldTypeKeyword,
		"classification": qdrant.FieldType_FieldTypeKeyword, "source": qdrant.FieldType_FieldTypeKeyword,
		"collected_at": qdrant.FieldType_FieldTypeDatetime, "tags": qdrant.FieldType_FieldTypeKeyword,
		"asset_refs": qdrant.FieldType_FieldTypeKeyword, "path": qdrant.FieldType_FieldTypeKeyword,
		"document_id": qdrant.FieldType_FieldTypeKeyword, "document_revision": qdrant.FieldType_FieldTypeKeyword,
		"scope_revision": qdrant.FieldType_FieldTypeKeyword, "chunk_ordinal": qdrant.FieldType_FieldTypeInteger,
	}
	controlIndexes := map[string]qdrant.FieldType{
		"hive_id": qdrant.FieldType_FieldTypeKeyword, "record_type": qdrant.FieldType_FieldTypeKeyword,
		"logical_key": qdrant.FieldType_FieldTypeKeyword, "program_id": qdrant.FieldType_FieldTypeKeyword,
		"state": qdrant.FieldType_FieldTypeKeyword, "status": qdrant.FieldType_FieldTypeKeyword,
	}
	for collection, indexes := range map[string]map[string]qdrant.FieldType{iw.Cfg.CollectionName: dataIndexes, iw.Cfg.ControlCollection: controlIndexes} {
		fields := make([]string, 0, len(indexes))
		for field := range indexes {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			fieldType := indexes[field]
			if _, err := iw.QdrantClient.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{CollectionName: collection, Wait: qdrant.PtrOf(true), FieldName: field, FieldType: &fieldType}); err != nil {
				return fmt.Errorf("create payload index %s.%s: %w", collection, field, err)
			}
		}
	}
	return nil
}

func (iw *IngestionWorker) expectedManifest(dimension int, digest string) map[string]any {
	return map[string]any{
		"record_type": "collection_manifest", "schema_version": int64(1),
		"hive_id": iw.Cfg.HiveID, "data_collection": iw.Cfg.CollectionName,
		"control_collection": iw.Cfg.ControlCollection, "embedding_provider": "ollama",
		"embedding_model": iw.Cfg.EmbeddingModel, "embedding_model_digest": digest,
		"vector_dimension": int64(dimension), "distance": "cosine", "normalization": "none",
		"parser_version": iw.parserFingerprint(), "chunker_version": iw.chunkerFingerprint(),
		"payload_schema_version": payloadSchemaVersion,
	}
}

func (iw *IngestionWorker) parserFingerprint() string {
	return fmt.Sprintf("%s:json-depth=%d:json-elements=%d", parserVersion, iw.Cfg.JSONMaxDepth, iw.Cfg.JSONMaxElements)
}

func (iw *IngestionWorker) chunkerFingerprint() string {
	return fmt.Sprintf("%s:max-chars=%d:overlap=%d:max-chunks=%d", chunkerVersion, iw.Cfg.ChunkMaxChars, iw.Cfg.ChunkOverlapChars, iw.Cfg.MaxChunksPerFile)
}

// ValidateInfrastructure is the read-only counterpart used by readers. It
// never creates a collection or writes a control record.
func (iw *IngestionWorker) ValidateInfrastructure(ctx context.Context) error {
	iw.infrastructureMu.Lock()
	defer iw.infrastructureMu.Unlock()
	if iw.infrastructureReady {
		return nil
	}
	dimensionVector, err := iw.FetchRemoteEmbedding(ctx, "hive-mind dimension probe")
	if err != nil {
		return fmt.Errorf("resolve embedding dimension: %w", err)
	}
	if len(dimensionVector) == 0 {
		return errors.New("embedding model returned an empty dimension probe")
	}
	digest, err := iw.fetchEmbeddingModelDigest(ctx)
	if err != nil {
		return err
	}
	for _, name := range []string{iw.Cfg.CollectionName, iw.Cfg.ControlCollection} {
		exists, err := iw.QdrantClient.CollectionExists(ctx, name)
		if err != nil {
			return fmt.Errorf("inspect collection %s: %w", name, err)
		}
		if !exists {
			return fmt.Errorf("required collection %s does not exist", name)
		}
	}
	if err := iw.validateExistingCollection(ctx, iw.Cfg.CollectionName, uint64(len(dimensionVector)), false); err != nil {
		return err
	}
	if err := iw.validateExistingCollection(ctx, iw.Cfg.ControlCollection, 0, true); err != nil {
		return err
	}
	rows, err := iw.controlRows(ctx, "collection_manifest", iw.Cfg.HiveID)
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return errors.New("exactly one collection manifest is required")
	}
	for field, value := range iw.expectedManifest(len(dimensionVector), digest) {
		if payloadComparable(rows[0].Payload[field]) != fmt.Sprint(value) {
			return fmt.Errorf("incompatible collection manifest field %s", field)
		}
	}
	registrations, err := iw.writerRegistrations(ctx)
	if err != nil {
		return err
	}
	if len(registrations) == 0 {
		return errors.New("at least one writer registration is required")
	}
	if iw.Cfg.IsWriter() {
		var own *qdrant.RetrievedPoint
		for _, row := range registrations {
			if payloadString(row.Payload, "writer_device_id", "") == iw.Cfg.DeviceID {
				own = row
				break
			}
		}
		if own == nil {
			return errors.New("this writer device is not registered; run ingest once to register it")
		}
		if payloadString(own.Payload, "operational_approval_id", "") != iw.Cfg.WriterApprovalID {
			return errors.New("writer registration approval does not match HIVE_WRITER_APPROVAL_ID")
		}
	}
	iw.infrastructureReady = true
	return nil
}

// writerRegistrations lists every registered writer device for this Hive.
func (iw *IngestionWorker) writerRegistrations(ctx context.Context) ([]*qdrant.RetrievedPoint, error) {
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: iw.Cfg.ControlCollection,
		Filter: &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("record_type", "writer_registration"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
		}}, Limit: qdrant.PtrOf(uint32(256)), WithPayload: qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	valid := make([]*qdrant.RetrievedPoint, 0, len(rows))
	for _, row := range rows {
		if payloadString(row.Payload, "writer_device_id", "") != "" {
			valid = append(valid, row)
		}
	}
	return valid, nil
}

// ValidateCredentialCapabilities actively proves that the configured Qdrant
// credential matches the immutable process role. Qdrant exposes no portable
// permission-introspection endpoint, so readers use a payload-only probe in the
// vectorless control collection and require an explicit PermissionDenied.
func (iw *IngestionWorker) ValidateCredentialCapabilities(ctx context.Context) error {
	if !iw.Cfg.IsWriter() && !iw.Cfg.IsReader() {
		return errors.New("authorization requires a valid Hive role")
	}
	for _, collection := range []string{iw.Cfg.CollectionName, iw.Cfg.ControlCollection} {
		if _, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: collection, Exact: qdrant.PtrOf(true)}); err != nil {
			return safeServiceError(err)
		}
		// Contradictory predicates can never match a point. Qdrant still
		// authorizes the write at collection level, without changing any data.
		_, err := iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{
			CollectionName: collection, Wait: qdrant.PtrOf(true),
			Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{
				Must:    []*qdrant.Condition{qdrant.NewMatchKeyword("record_type", "credential_probe")},
				MustNot: []*qdrant.Condition{qdrant.NewMatchKeyword("record_type", "credential_probe")},
			}),
		})
		if iw.Cfg.IsReader() {
			if err == nil {
				return errors.New("Qdrant reader credential permits writes")
			}
			if status.Code(err) != codes.PermissionDenied {
				return fmt.Errorf("reader write-denial check failed: %w", safeServiceError(err))
			}
		} else if err != nil {
			return fmt.Errorf("writer write check failed: %w", safeServiceError(err))
		}
	}
	if iw.Cfg.IsReader() {
		return nil
	}

	probeID := deterministicUUID("credential-probe", iw.Cfg.HiveID, iw.Cfg.DeviceID)
	probeFilter := &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "credential_probe"),
		qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
		qdrant.NewMatchKeyword("device_id", iw.Cfg.DeviceID),
	}}
	_, writeErr := iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: iw.Cfg.ControlCollection, Wait: qdrant.PtrOf(true),
		Points: []*qdrant.PointStruct{{Id: qdrant.NewIDUUID(probeID), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{}), Payload: qdrant.NewValueMap(map[string]any{
			"record_type": "credential_probe", "hive_id": iw.Cfg.HiveID, "device_id": iw.Cfg.DeviceID,
		})}},
	})
	if writeErr != nil {
		if status.Code(writeErr) == codes.Unauthenticated || status.Code(writeErr) == codes.PermissionDenied {
			return errors.New("Qdrant writer credential lacks read-write permission")
		}
		return errors.New("Qdrant writer write check failed")
	}
	if _, err := iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.ControlCollection, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(probeFilter)}); err != nil {
		return errors.New("Qdrant writer credential cannot remove its capability probe")
	}
	return nil
}

func (iw *IngestionWorker) validateExistingCollection(ctx context.Context, name string, dimension uint64, vectorless bool) error {
	inspector, ok := iw.QdrantClient.(interface {
		GetCollectionInfo(context.Context, string) (*qdrant.CollectionInfo, error)
	})
	if !ok {
		return nil
	}
	info, err := inspector.GetCollectionInfo(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect collection schema %s: %w", name, err)
	}
	params := info.GetConfig().GetParams()
	vectors := params.GetVectorsConfig()
	if vectorless {
		if vectors.GetParams() != nil || (vectors.GetParamsMap() != nil && len(vectors.GetParamsMap().GetMap()) != 0) {
			return fmt.Errorf("control collection %s is not vectorless", name)
		}
		return nil
	}
	dense := vectors.GetParams()
	if dense == nil || dense.GetSize() != dimension || dense.GetDistance() != qdrant.Distance_Cosine {
		return fmt.Errorf("data collection %s has an incompatible vector schema", name)
	}
	if params.GetSparseVectorsConfig() == nil || params.GetSparseVectorsConfig().GetMap()["sparse"] == nil {
		return fmt.Errorf("data collection %s has no sparse vector schema", name)
	}
	return nil
}

func (iw *IngestionWorker) fetchEmbeddingModelDigest(ctx context.Context) (string, error) {
	policy := serviceRetryPolicy.normalized()
	retryCtx, cancel := boundedRetryContext(ctx, policy.OperationTimeout)
	defer cancel()
	var resp *http.Response
	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		req, err := httpRequest(retryCtx, "GET", iw.Cfg.OllamaHost+"/api/tags", nil)
		if err != nil {
			return "", err
		}
		resp, err = iw.HTTPClient.Do(req)
		if err == nil && !retryableHTTPStatus(resp.StatusCode) {
			break
		}
		if err != nil {
			lastErr = err
			if resp != nil {
				closeRetryResponse(resp.Body)
				resp = nil
			}
		} else {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			closeRetryResponse(resp.Body)
			resp = nil
		}
		if attempt == policy.MaxAttempts {
			return "", fmt.Errorf("resolve embedding model digest after %d attempts: %w", policy.MaxAttempts, lastErr)
		}
		if err := waitForRetry(retryCtx, backoffDelay(policy, attempt)); err != nil {
			return "", err
		}
	}
	if resp == nil {
		return "", fmt.Errorf("resolve embedding model digest: %w", lastErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve embedding model digest: HTTP %d", resp.StatusCode)
	}
	var tags ollamaTagsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&tags); err != nil {
		return "", fmt.Errorf("decode embedding model identity: %w", err)
	}
	wanted := iw.Cfg.EmbeddingModel
	for _, model := range tags.Models {
		if model.Name == wanted || model.Model == wanted || strings.TrimSuffix(model.Name, ":latest") == strings.TrimSuffix(wanted, ":latest") {
			if len(model.Digest) < 16 {
				return "", errors.New("embedding model has no valid digest")
			}
			return model.Digest, nil
		}
	}
	return "", fmt.Errorf("embedding model %q is not installed", wanted)
}

func httpRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, body)
}

func (iw *IngestionWorker) ensureImmutableControl(ctx context.Context, recordType, key string, expected map[string]any) error {
	rows, err := iw.controlRows(ctx, recordType, key)
	if err != nil {
		return err
	}
	if len(rows) > 1 {
		return errors.New("duplicate control records")
	}
	if len(rows) == 1 {
		for field, value := range expected {
			if payloadComparable(rows[0].Payload[field]) != fmt.Sprint(value) {
				return fmt.Errorf("incompatible %s field %s", recordType, field)
			}
		}
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	expected["created_at"] = now
	if recordType == "writer_registration" {
		expected["assigned_at"] = now
	}
	return iw.upsertControl(ctx, recordType, key, expected)
}

func payloadComparable(value *qdrant.Value) string {
	if value == nil {
		return ""
	}
	switch value.Kind.(type) {
	case *qdrant.Value_IntegerValue:
		return fmt.Sprint(value.GetIntegerValue())
	case *qdrant.Value_DoubleValue:
		return fmt.Sprint(value.GetDoubleValue())
	case *qdrant.Value_BoolValue:
		return fmt.Sprint(value.GetBoolValue())
	default:
		return value.GetStringValue()
	}
}

func (iw *IngestionWorker) controlRows(ctx context.Context, recordType, key string) ([]*qdrant.RetrievedPoint, error) {
	return iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: iw.Cfg.ControlCollection,
		Filter: &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("record_type", recordType), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
			qdrant.NewMatchKeyword("logical_key", key),
		}}, Limit: qdrant.PtrOf(uint32(2)), WithPayload: qdrant.NewWithPayloadEnable(true),
	})
}

func (iw *IngestionWorker) upsertControl(ctx context.Context, recordType, key string, payload map[string]any) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("control mutation requires HIVE_ROLE=writer")
	}
	rows, err := iw.controlRows(ctx, recordType, key)
	if err != nil {
		return err
	}
	if len(rows) > 1 {
		return errors.New("duplicate control records")
	}
	version := int64(1)
	var updateFilter *qdrant.Filter
	if len(rows) == 1 {
		oldVersion := payloadInt(rows[0].Payload, "control_version")
		if oldVersion < 1 {
			return errors.New("control record has no valid version")
		}
		version = oldVersion + 1
		updateFilter = &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("record_type", recordType), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
			qdrant.NewMatchKeyword("logical_key", key), qdrant.NewMatchInt("control_version", oldVersion),
		}}
	}
	payload["record_type"], payload["hive_id"], payload["logical_key"] = recordType, iw.Cfg.HiveID, key
	payload["control_version"] = version
	if err := iw.auditControl(recordType, payload, "prepared"); err != nil {
		return err
	}
	id := deterministicUUID("control", iw.Cfg.HiveID, recordType, key)
	_, err = iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: iw.Cfg.ControlCollection, Wait: qdrant.PtrOf(true),
		// gRPC requires an explicit empty vector map for payload-only points.
		UpdateFilter: updateFilter, Points: []*qdrant.PointStruct{{Id: qdrant.NewIDUUID(id), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{}), Payload: qdrant.NewValueMap(payload)}},
	})
	if err != nil {
		return err
	}
	committed, err := iw.controlRows(ctx, recordType, key)
	if err != nil {
		return err
	}
	if len(committed) != 1 || payloadInt(committed[0].Payload, "control_version") != version {
		return errors.New("optimistic control write conflicted")
	}
	for field, value := range payload {
		if payloadComparable(committed[0].Payload[field]) != fmt.Sprint(value) {
			return errors.New("optimistic control write conflicted")
		}
	}
	return iw.auditControl(recordType, payload, "committed")
}

func deterministicUUID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00"))).String()
}

// SyncFileState publishes one complete immutable revision and only then moves the head.
func (iw *IngestionWorker) SyncFileState(ctx context.Context, path string) error {
	_, err := iw.syncFileResult(ctx, path)
	return err
}

// syncFileResult records the outcome at the publication boundary, without a
// second read of the head that could race with another synchronization.
func (iw *IngestionWorker) syncFileResult(ctx context.Context, path string) (result SyncFileResult, resultErr error) {
	result = SyncFileResult{Path: iw.syncReportPath(path), Outcome: SyncFailed}
	reason := "synchronization_failed"
	defer func() {
		if resultErr == nil {
			return
		}
		switch {
		case errors.Is(resultErr, errForeignDocument):
			result.Outcome, reason = SyncSkipped, "foreign_writer"
		case errors.Is(resultErr, context.Canceled), errors.Is(resultErr, context.DeadlineExceeded):
			result.Outcome, reason = SyncCancelled, "cancelled"
		default:
			result.Outcome = SyncFailed
			var inputErr *documentInputError
			if errors.As(resultErr, &inputErr) {
				reason = inputErr.reason
			}
		}
		result.ReasonCode, result.Detail = reason, syncReasonDetail(reason)
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !iw.Cfg.IsWriter() {
		reason = "writer_required"
		return result, errors.New("file synchronization requires HIVE_ROLE=writer")
	}
	if iw.ShouldIgnoreFile(path, false) {
		result.Outcome, result.ReasonCode, result.Detail = SyncSkipped, "ignored", syncReasonDetail("ignored")
		return result, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		result.Outcome, result.ReasonCode, result.Detail = SyncMissing, "missing_file", syncReasonDetail("missing_file")
		reason = "pending_delete_failed"
		return result, iw.MarkPendingDelete(ctx, path)
	}
	reason = "infrastructure_failed"
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return result, err
	}

	reason = "document_read_failed"
	content, relPath, programID, info, err := iw.secureReadDocument(path)
	if err != nil {
		return result, err
	}
	reason = "scope_unavailable"
	scopeRevision, scopeManifest, err := iw.resolveActiveScope(ctx, programID)
	if err != nil {
		return result, err
	}
	reason = "invalid_metadata"
	metadata, err := extractReconMetadata(relPath, programID, content)
	if err != nil {
		return result, fmt.Errorf("extract recon metadata: %w", err)
	}
	if metadata.Classification == "unknown" {
		result.Warnings = []string{"classification_unknown: document will not be returned by search"}
	}
	reason = "invalid_document"
	chunks, converted, err := iw.prepareDocument(ctx, relPath, content)
	if err != nil {
		return result, err
	}
	if len(chunks) == 0 {
		reason = "empty_document"
		return result, errors.New("document produced no chunks")
	}
	if len(chunks) > iw.Cfg.MaxChunksPerFile {
		reason = "chunk_limit"
		return result, fmt.Errorf("document exceeds %d chunks", iw.Cfg.MaxChunksPerFile)
	}

	contentHash := sha256Hex(content)
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, relPath)
	revisionParts := []string{contentHash, iw.parserFingerprint(), iw.chunkerFingerprint()}
	if converted != nil {
		// New adapters have their own version domain. Existing MD/TXT/JSON
		// collections and unchanged legacy documents keep their fingerprints.
		revisionParts = append(revisionParts, converted.ConverterFingerprint, "hive-document-projection-v1")
	}
	documentRevision := sha256Hex([]byte(strings.Join(revisionParts, "\x00")))
	effectiveScope := effectiveScopeStatus(scopeManifest, metadata.AssetRefs)
	reason = "control_unavailable"
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil {
		return result, err
	}
	if !iw.ownsHead(head) {
		_ = iw.audit(AuditEvent{Action: "document_skipped", Outcome: "foreign_writer", Program: programID, Document: documentID, Path: relPath}, false)
		return result, errForeignDocument
	}
	tombstoned, err := iw.isTombstoned(ctx, documentID)
	if err != nil {
		return result, err
	}
	if tombstoned {
		reason = "tombstoned"
		return result, errors.New("document is tombstoned; explicit revocation is required before reingestion")
	}
	if head != nil && head.State == "deleted" {
		reason = "tombstoned"
		return result, errors.New("document is tombstoned; explicit revocation is required before reingestion")
	}
	if head != nil && head.DocumentRevision == documentRevision && head.ScopeRevision == scopeRevision {
		if head.State == "active" {
			result.Outcome = SyncUnchanged
			return result, nil
		}
		if head.State == "pending_delete" {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			revived := *head
			revived.State, revived.PendingSince, revived.WriterDeviceID, revived.Bytes = "active", "", iw.Cfg.DeviceID, int64(len(content))
			reason = "commit_failed"
			if err := iw.upsertControl(ctx, "document_head", documentID, headPayload(&revived, now)); err != nil {
				return result, err
			}
			result.Outcome, result.Published = SyncUpdated, true
			return result, nil
		}
	}

	indexedAt := time.Now().UTC().Format(time.RFC3339Nano)
	points := make([]*qdrant.PointStruct, len(chunks))
	reason = "embedding_failed"
	for ordinal, chunk := range chunks {
		vector, err := iw.FetchRemoteEmbedding(ctx, chunk.Text)
		if err != nil {
			return result, fmt.Errorf("embed chunk %d: %w", ordinal, err)
		}
		indices, values := ComputeSparseVector(chunk.Text, iw.CustomStopWords)
		pointID := deterministicUUID("chunk", documentID, documentRevision, scopeRevision, fmt.Sprint(ordinal))
		payload := map[string]any{
			"record_type": "chunk", "hive_id": iw.Cfg.HiveID, "program_id": programID,
			"document_type":        metadata.DocumentType,
			"claimed_scope_status": metadata.ClaimedScope, "effective_scope_status": effectiveScope,
			"classification": metadata.Classification, "source": metadata.Source, "collected_at": metadata.CollectedAt,
			"tags": convertStringSlice(metadata.Tags), "asset_refs": convertStringSlice(metadata.AssetRefPayload), "path": relPath,
			"file_path": relPath, "relative_path": relPath, "content": chunk.Text,
			"document_id": documentID, "document_revision": documentRevision,
			"scope_revision": scopeRevision, "chunk_ordinal": int64(ordinal),
			"content_hash": contentHash, "indexed_at": indexedAt, "modified": info.ModTime().Unix(),
			"type": "doc_chunk", "extension": strings.TrimPrefix(filepath.Ext(relPath), "."),
		}
		if converted != nil {
			payload["ingestion_schema"] = converted.Schema
			payload["source_format"] = converted.SourceFormat
			payload["source_raw_hash"] = converted.RawHash
			payload["converter_fingerprint"] = converted.ConverterFingerprint
			payload["source_locator"] = chunk.Locator
			payload["block_ordinal"] = int64(chunk.BlockOrdinal)
			payload["canonical_hash"] = chunk.CanonicalHash
			payload["writer_device_id"] = iw.Cfg.DeviceID
			payload["untrusted_content"] = true
		}
		points[ordinal] = &qdrant.PointStruct{Id: qdrant.NewIDUUID(pointID), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{
			"": qdrant.NewVector(vector...), "sparse": qdrant.NewVectorSparse(indices, values),
		}), Payload: qdrant.NewValueMap(payload)}
	}

	reason = "staging_failed"
	if _, err := iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: points}); err != nil {
		return result, fmt.Errorf("stage document revision: %w", err)
	}
	reason = "verification_failed"
	count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.CollectionName, Exact: qdrant.PtrOf(true), Filter: revisionFilter(documentID, documentRevision, scopeRevision)})
	if err != nil {
		return result, fmt.Errorf("verify document revision: %w", err)
	}
	if count != uint64(len(points)) {
		return result, fmt.Errorf("verify document revision: expected %d chunks, found %d", len(points), count)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	created := now
	if head != nil && head.CreatedAt != "" {
		created = head.CreatedAt
	}
	reason = "commit_failed"
	if err := iw.upsertControl(ctx, "document_head", documentID, headPayload(&documentHead{
		DocumentID: documentID, ProgramID: programID, Path: relPath,
		DocumentRevision: documentRevision, ScopeRevision: scopeRevision,
		ChunkCount: len(points), State: "active", CreatedAt: created,
		WriterDeviceID: iw.Cfg.DeviceID, Bytes: int64(len(content)),
	}, now)); err != nil {
		return result, fmt.Errorf("commit document head: %w", err)
	}
	result.Outcome, result.Published = SyncCreated, true
	if head != nil {
		result.Outcome = SyncUpdated
	}

	reason = "cleanup_pending"
	if head != nil && (head.DocumentRevision != documentRevision || head.ScopeRevision != scopeRevision) {
		if _, err := iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(revisionFilter(documentID, head.DocumentRevision, head.ScopeRevision))}); err != nil {
			return result, fmt.Errorf("new revision active; stale revision cleanup pending: %w", err)
		}
	}
	return result, nil
}

func revisionFilter(documentID, documentRevision, scopeRevision string) *qdrant.Filter {
	return &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "chunk"), qdrant.NewMatchKeyword("document_id", documentID),
		qdrant.NewMatchKeyword("document_revision", documentRevision), qdrant.NewMatchKeyword("scope_revision", scopeRevision),
	}}
}

func (iw *IngestionWorker) readDocumentHead(ctx context.Context, documentID string) (*documentHead, error) {
	rows, err := iw.controlRows(ctx, "document_head", documentID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, errors.New("duplicate document heads")
	}
	p := rows[0].Payload
	return &documentHead{DocumentID: documentID, ProgramID: payloadString(p, "program_id", ""), Path: payloadString(p, "path", ""),
		DocumentRevision: payloadString(p, "active_document_revision", ""), ScopeRevision: payloadString(p, "active_scope_revision", ""),
		ChunkCount: int(payloadInt(p, "chunk_count")), State: payloadString(p, "state", ""), PendingSince: payloadString(p, "pending_since", ""),
		CreatedAt: payloadString(p, "created_at", ""), WriterDeviceID: payloadString(p, "writer_device_id", ""), Bytes: payloadInt(p, "document_bytes"),
	}, nil
}

func (iw *IngestionWorker) isTombstoned(ctx context.Context, documentID string) (bool, error) {
	rows, err := iw.controlRows(ctx, "tombstone", documentID)
	if err != nil {
		return false, err
	}
	if len(rows) > 1 {
		return false, errors.New("duplicate tombstones")
	}
	return len(rows) == 1, nil
}

func (iw *IngestionWorker) filterActiveCandidates(ctx context.Context, points []*qdrant.ScoredPoint) ([]*qdrant.ScoredPoint, error) {
	filtered, _, err := iw.filterActiveCandidatesWithHeads(ctx, points)
	return filtered, err
}

// filterActiveCandidatesWithHeads also returns the heads it loaded, keyed by
// document_id, so callers can report document sizes without another round trip.
func (iw *IngestionWorker) filterActiveCandidatesWithHeads(ctx context.Context, points []*qdrant.ScoredPoint) ([]*qdrant.ScoredPoint, map[string]*documentHead, error) {
	cache := make(map[string]*documentHead)
	tombstoneCache := make(map[string]bool)
	scopeCache := make(map[string]string)
	filtered := make([]*qdrant.ScoredPoint, 0, len(points))
	for _, point := range points {
		payload := point.Payload
		if payloadString(payload, "record_type", "") != "chunk" || payloadString(payload, "hive_id", "") != iw.Cfg.HiveID || payloadString(payload, "program_id", "") == "" {
			continue
		}
		documentID := payloadString(payload, "document_id", "")
		tombstoned, checked := tombstoneCache[documentID]
		if !checked {
			var err error
			tombstoned, err = iw.isTombstoned(ctx, documentID)
			if err != nil {
				return nil, nil, err
			}
			tombstoneCache[documentID] = tombstoned
		}
		if tombstoned {
			continue
		}
		head, exists := cache[documentID]
		if !exists {
			var err error
			head, err = iw.readDocumentHead(ctx, documentID)
			if err != nil {
				return nil, nil, err
			}
			cache[documentID] = head
		}
		programID := payloadString(payload, "program_id", "")
		expectedScope, checked := scopeCache[programID]
		if !checked {
			var err error
			expectedScope, err = iw.controlScopeRevision(ctx, programID)
			if err != nil {
				return nil, nil, err
			}
			scopeCache[programID] = expectedScope
		}
		if head == nil || head.State == "deleted" || head.ProgramID != programID ||
			head.DocumentRevision != payloadString(payload, "document_revision", "") ||
			head.ScopeRevision != payloadString(payload, "scope_revision", "") || expectedScope != payloadString(payload, "scope_revision", "") {
			continue
		}
		filtered = append(filtered, point)
	}
	return filtered, cache, nil
}

func (iw *IngestionWorker) secureReadDocument(path string) ([]byte, string, string, os.FileInfo, error) {
	relPath, programID, err := iw.logicalDocumentPath(path)
	if err != nil {
		return nil, "", "", nil, err
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if !supportedDocumentExtension(ext) {
		return nil, "", "", nil, &documentInputError{"unsupported_format", fmt.Sprintf("unsupported document extension %q", ext)}
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", "", nil, errors.New("document path cannot be resolved")
	}
	if !pathWithin(iw.Cfg.DataDirectory, real) {
		return nil, "", "", nil, errors.New("document resolves outside HIVE_DATA_DIR")
	}
	before, err := os.Stat(real)
	if err != nil || !before.Mode().IsRegular() {
		return nil, "", "", nil, errors.New("document is not a regular file")
	}
	if before.Size() > iw.Cfg.MaxFileSize {
		return nil, "", "", nil, &documentInputError{"file_size_limit", fmt.Sprintf("document exceeds %d bytes", iw.Cfg.MaxFileSize)}
	}
	file, err := os.Open(real)
	if err != nil {
		return nil, "", "", nil, errors.New("document cannot be opened")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", "", nil, errors.New("document changed during secure open")
	}
	content, err := io.ReadAll(io.LimitReader(file, iw.Cfg.MaxFileSize+1))
	if err != nil {
		return nil, "", "", nil, errors.New("document cannot be read")
	}
	if int64(len(content)) > iw.Cfg.MaxFileSize {
		return nil, "", "", nil, &documentInputError{"file_size_limit", fmt.Sprintf("document exceeds %d bytes", iw.Cfg.MaxFileSize)}
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, "", "", nil, errors.New("document changed while being read")
	}
	if len(content) == 0 {
		return nil, "", "", nil, &documentInputError{"empty_document", "document is empty"}
	}
	if !utf8.Valid(content) || isBinaryContent(content) {
		return nil, "", "", nil, &documentInputError{"invalid_encoding", "binary or non-UTF-8 document is not supported"}
	}
	return content, relPath, programID, after, nil
}

func (iw *IngestionWorker) logicalDocumentPath(path string) (string, string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(iw.Cfg.DataDirectory, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil || !pathWithin(iw.Cfg.DataDirectory, abs) {
		return "", "", errors.New("document path escapes HIVE_DATA_DIR")
	}
	rel, err := filepath.Rel(iw.Cfg.DataDirectory, abs)
	if err != nil {
		return "", "", errors.New("document path cannot be normalized")
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "programs" || !validIdentifier(parts[1], 64) {
		return "", "", errors.New("document must be under programs/<program_id>/")
	}
	return rel, parts[1], nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (iw *IngestionWorker) parseDocument(path string, content []byte) ([]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return chunkRunes(string(content), iw.Cfg.ChunkMaxChars, iw.Cfg.ChunkOverlapChars), nil
	case ".md":
		return chunkMarkdown(string(content), iw.Cfg.ChunkMaxChars, iw.Cfg.ChunkOverlapChars), nil
	case ".json":
		normalized, err := normalizeJSON(content, iw.Cfg.JSONMaxDepth, iw.Cfg.JSONMaxElements)
		if err != nil {
			return nil, err
		}
		return chunkRunes(normalized, iw.Cfg.ChunkMaxChars, iw.Cfg.ChunkOverlapChars), nil
	default:
		return nil, errors.New("unsupported document type")
	}
}

// prepareDocument is shared by CLI ingestion, watcher and MCP. Legacy parsing
// stays byte-compatible; the versioned envelope and new formats use adapters.
func (iw *IngestionWorker) prepareDocument(ctx context.Context, path string, content []byte) ([]ConvertedChunk, *ConvertedDocument, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var converted *ConvertedDocument
	var err error
	if ext == ".json" {
		converted, err = DecodeConvertedDocument(content, iw.Cfg)
		if err != nil {
			return nil, nil, err
		}
	} else if ext != ".md" && ext != ".txt" {
		doc, convertErr := ConvertDocument(ctx, strings.TrimPrefix(ext, "."), content, iw.Cfg)
		if convertErr != nil {
			return nil, nil, convertErr
		}
		converted = &doc
	}
	if converted != nil {
		chunks, err := ChunkConvertedDocument(ctx, *converted, iw.Cfg)
		return chunks, converted, err
	}
	texts, err := iw.parseDocument(path, content)
	if err != nil {
		return nil, nil, err
	}
	chunks := make([]ConvertedChunk, len(texts))
	for i, text := range texts {
		chunks[i].Text = text
	}
	return chunks, nil, nil
}

func chunkRunes(text string, maxChars, overlap int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	step := maxChars - overlap
	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func chunkMarkdown(text string, maxChars, overlap int) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	prefixEnd := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				prefixEnd = i + 1
				break
			}
		}
	}
	title := ""
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			title = line
			break
		}
	}
	metadata := strings.TrimSpace(strings.Join(lines[:prefixEnd], "\n"))
	prefix := metadata
	if title != "" && !strings.Contains(prefix, title) {
		prefix = strings.TrimSpace(prefix + "\n" + title)
	}
	var sections []string
	var current []string
	for _, line := range lines[prefixEnd:] {
		if strings.HasPrefix(strings.TrimSpace(line), "#") && len(current) > 0 {
			sections = append(sections, strings.TrimSpace(strings.Join(current, "\n")))
			current = nil
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		sections = append(sections, strings.TrimSpace(strings.Join(current, "\n")))
	}
	var out []string
	for _, section := range sections {
		body := strings.TrimSpace(section)
		if title != "" && strings.HasPrefix(body, title) && metadata != "" {
			body = metadata + "\n" + body
		} else if prefix != "" && !strings.HasPrefix(body, prefix) {
			body = prefix + "\n\n" + body
		}
		out = append(out, chunkRunes(body, maxChars, overlap)...)
	}
	return out
}

func normalizeJSON(content []byte, maxDepth, maxElements int) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", errors.New("invalid JSON: trailing value")
	}
	count := 0
	var validate func(any, int) error
	validate = func(v any, depth int) error {
		if depth > maxDepth {
			return fmt.Errorf("JSON exceeds depth %d", maxDepth)
		}
		count++
		if count > maxElements {
			return fmt.Errorf("JSON exceeds %d elements", maxElements)
		}
		switch item := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if err := validate(item[key], depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range item {
				if err := validate(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := validate(value, 1); err != nil {
		return "", err
	}
	normalized, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", errors.New("JSON normalization failed")
	}
	return string(normalized), nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// MarkPendingDelete records absence without deleting searchable data.
func (iw *IngestionWorker) MarkPendingDelete(ctx context.Context, path string) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("document removal requires HIVE_ROLE=writer")
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return err
	}
	rel, programID, err := iw.logicalDocumentPath(path)
	if err != nil {
		return err
	}
	if rel == filepath.ToSlash(filepath.Join("programs", programID, "scope.json")) {
		if _, _, err := iw.resolveActiveScope(ctx, programID); err != nil {
			return err
		}
	}
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, rel)
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil || head == nil {
		return err
	}
	if !iw.ownsHead(head) {
		return errForeignDocument
	}
	if head.State == "deleted" || head.State == "pending_delete" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	pending := *head
	pending.State, pending.PendingSince, pending.WriterDeviceID = "pending_delete", now, iw.Cfg.DeviceID
	return iw.upsertControl(ctx, "document_head", documentID, headPayload(&pending, now))
}

// RemoveDocument confirms intent with a tombstone before deleting chunks.
func (iw *IngestionWorker) RemoveDocument(ctx context.Context, path, reason string) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("document removal requires HIVE_ROLE=writer")
	}
	if reason == "" {
		reason = "operator_requested"
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return err
	}
	rel, programID, err := iw.logicalDocumentPath(path)
	if err != nil {
		return &operationalError{ExitUsage, "invalid document path"}
	}
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, rel)
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil || head == nil {
		return err
	}
	if !iw.ownsHead(head) {
		return &operationalError{ExitAuthorization, "document is owned by another writer"}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := iw.upsertControl(ctx, "tombstone", documentID, map[string]any{
		"program_id": head.ProgramID, "path": head.Path,
		"document_id": documentID, "last_document_revision": head.DocumentRevision,
		"reason": reason, "deleted_by_device_id": iw.Cfg.DeviceID, "deleted_at": now,
	}); err != nil {
		return fmt.Errorf("write tombstone: %w", err)
	}
	deleted := *head
	deleted.State, deleted.PendingSince, deleted.WriterDeviceID = "deleted", "", iw.Cfg.DeviceID
	if err := iw.upsertControl(ctx, "document_head", documentID, headPayload(&deleted, now)); err != nil {
		return fmt.Errorf("commit deleted head: %w", err)
	}
	_, err = iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{Must: []*qdrant.Condition{qdrant.NewMatchKeyword("document_id", documentID)}})})
	if err != nil {
		return &operationalError{ExitPartialFailure, "tombstone committed; deletion incomplete"}
	}
	remaining, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.CollectionName, Exact: qdrant.PtrOf(true), Filter: &qdrant.Filter{Must: []*qdrant.Condition{qdrant.NewMatchKeyword("document_id", documentID)}}})
	if err != nil || remaining != 0 {
		return &operationalError{ExitPartialFailure, "tombstone committed; deletion verification incomplete"}
	}
	return iw.audit(AuditEvent{Action: "document_removal", Outcome: "committed", Document: documentID, Program: programID, Path: rel}, true)
}

// PrunePending removes only heads whose grace period has elapsed.
func (iw *IngestionWorker) PrunePending(ctx context.Context, now time.Time) (int, error) {
	if !iw.Cfg.IsWriter() {
		return 0, errors.New("prune requires HIVE_ROLE=writer")
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return 0, err
	}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{CollectionName: iw.Cfg.ControlCollection,
		Filter: &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("record_type", "document_head"), qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID), qdrant.NewMatchKeyword("state", "pending_delete"),
		}}, Limit: qdrant.PtrOf(uint32(5000)), WithPayload: qdrant.NewWithPayloadEnable(true)})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, row := range rows {
		if owner := payloadString(row.Payload, "writer_device_id", ""); owner != "" && owner != iw.Cfg.DeviceID {
			continue
		}
		pendingSince, err := time.Parse(time.RFC3339Nano, payloadString(row.Payload, "pending_since", ""))
		if err != nil {
			return removed, errors.New("invalid pending_delete timestamp")
		}
		if now.UTC().Before(pendingSince.Add(iw.Cfg.DeleteGrace)) {
			continue
		}
		path := filepath.Join(iw.Cfg.DataDirectory, filepath.FromSlash(payloadString(row.Payload, "path", "")))
		if err := iw.RemoveDocument(ctx, path, "grace_period_elapsed"); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (iw *IngestionWorker) SyncWorkspace(ctx context.Context) (SyncSummary, error) {
	summary := SyncSummary{Results: []SyncFileResult{}}
	if !iw.Cfg.IsWriter() {
		return summary, &operationalError{ExitAuthorization, "workspace ingestion requires HIVE_ROLE=writer"}
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return summary, err
	}
	var paths []string
	err := filepath.WalkDir(iw.Cfg.DataDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if iw.ShouldIgnoreFile(path, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if supportedDocumentExtension(ext) {
				paths = append(paths, path)
			}
		}
		return nil
	})
	if err != nil {
		return summary, err
	}
	sort.Strings(paths)
	summary.ScanComplete = true
	summary.Total = len(paths)
	summary.Results = make([]SyncFileResult, len(paths))
	for i, path := range paths {
		summary.Results[i] = SyncFileResult{Path: iw.syncReportPath(path), Outcome: SyncCancelled,
			ReasonCode: "cancelled", Detail: syncReasonDetail("cancelled")}
	}
	workers := iw.Cfg.MaxEmbeddingWorkers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				// Each job owns one slot; aggregate only after every worker exits.
				summary.Results[index], _ = iw.syncFileResult(ctx, paths[index])
			}
		}()
	}
sendLoop:
	for index := range paths {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- index:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	for _, result := range summary.Results {
		switch result.Outcome {
		case SyncCreated:
			summary.Created++
		case SyncUpdated:
			summary.Updated++
		case SyncUnchanged:
			summary.Unchanged++
		case SyncSkipped:
			summary.Skipped++
		case SyncMissing:
			summary.Missing++
		case SyncCancelled:
			summary.Cancelled++
		case SyncFailed:
			summary.Failed++
		}
	}
	summary.Ingested = summary.Created + summary.Updated
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if summary.Failed > 0 || summary.Cancelled > 0 {
		return summary, &operationalError{ExitPartialFailure, "workspace ingestion is incomplete; inspect per-file results"}
	}
	return summary, nil
}

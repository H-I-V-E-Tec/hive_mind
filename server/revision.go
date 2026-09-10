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
	if err := iw.ensureImmutableControl(ctx, "writer_registration", iw.Cfg.HiveID, registration); err != nil {
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
	registrations, err := iw.controlRows(ctx, "writer_registration", iw.Cfg.HiveID)
	if err != nil {
		return err
	}
	if len(registrations) != 1 || payloadString(registrations[0].Payload, "writer_device_id", "") == "" {
		return errors.New("exactly one writer registration is required")
	}
	iw.infrastructureReady = true
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
	req, err := httpRequest(ctx, "GET", iw.Cfg.OllamaHost+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	resp, err := iw.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve embedding model digest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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
		if recordType == "writer_registration" && payloadString(rows[0].Payload, "writer_device_id", "") != iw.Cfg.DeviceID {
			return errors.New("another writer is registered for this Hive")
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
	id := deterministicUUID("control", iw.Cfg.HiveID, recordType, key)
	_, err = iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: iw.Cfg.ControlCollection, Wait: qdrant.PtrOf(true),
		UpdateFilter: updateFilter, Points: []*qdrant.PointStruct{{Id: qdrant.NewIDUUID(id), Payload: qdrant.NewValueMap(payload)}},
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
	return nil
}

func deterministicUUID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00"))).String()
}

// SyncFileState publishes one complete immutable revision and only then moves the head.
func (iw *IngestionWorker) SyncFileState(ctx context.Context, path string) error {
	if !iw.Cfg.IsWriter() {
		return errors.New("file synchronization requires HIVE_ROLE=writer")
	}
	if iw.ShouldIgnoreFile(path, false) {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return iw.MarkPendingDelete(ctx, path)
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return err
	}

	content, relPath, programID, info, err := iw.secureReadDocument(path)
	if err != nil {
		return err
	}
	scopeRevision, scopeManifest, err := iw.resolveActiveScope(ctx, programID)
	if err != nil {
		return err
	}
	metadata, err := extractReconMetadata(relPath, programID, content)
	if err != nil {
		return fmt.Errorf("extract recon metadata: %w", err)
	}
	chunks, err := iw.parseDocument(relPath, content)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return errors.New("document produced no chunks")
	}
	if len(chunks) > iw.Cfg.MaxChunksPerFile {
		return fmt.Errorf("document exceeds %d chunks", iw.Cfg.MaxChunksPerFile)
	}

	contentHash := sha256Hex(content)
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, relPath)
	documentRevision := sha256Hex([]byte(strings.Join([]string{contentHash, iw.parserFingerprint(), iw.chunkerFingerprint()}, "\x00")))
	effectiveScope := effectiveScopeStatus(scopeManifest, metadata.AssetRefs)
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil {
		return err
	}
	tombstoned, err := iw.isTombstoned(ctx, documentID)
	if err != nil {
		return err
	}
	if tombstoned {
		return errors.New("document is tombstoned; explicit revocation is required before reingestion")
	}
	if head != nil && head.State == "deleted" {
		return errors.New("document is tombstoned; explicit revocation is required before reingestion")
	}
	if head != nil && head.DocumentRevision == documentRevision && head.ScopeRevision == scopeRevision {
		if head.State == "active" {
			return nil
		}
		if head.State == "pending_delete" {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			return iw.upsertControl(ctx, "document_head", documentID, map[string]any{
				"document_id": documentID, "program_id": head.ProgramID, "path": head.Path,
				"active_document_revision": head.DocumentRevision, "active_scope_revision": head.ScopeRevision,
				"chunk_count": int64(head.ChunkCount), "state": "active", "created_at": head.CreatedAt, "updated_at": now,
			})
		}
	}

	indexedAt := time.Now().UTC().Format(time.RFC3339Nano)
	points := make([]*qdrant.PointStruct, len(chunks))
	for ordinal, chunk := range chunks {
		vector, err := iw.FetchRemoteEmbedding(ctx, chunk)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", ordinal, err)
		}
		indices, values := ComputeSparseVector(chunk, iw.CustomStopWords)
		pointID := deterministicUUID("chunk", documentID, documentRevision, scopeRevision, fmt.Sprint(ordinal))
		payload := map[string]any{
			"record_type": "chunk", "hive_id": iw.Cfg.HiveID, "program_id": programID,
			"document_type":        metadata.DocumentType,
			"claimed_scope_status": metadata.ClaimedScope, "effective_scope_status": effectiveScope,
			"classification": metadata.Classification, "source": metadata.Source, "collected_at": metadata.CollectedAt,
			"tags": convertStringSlice(metadata.Tags), "asset_refs": convertStringSlice(metadata.AssetRefPayload), "path": relPath,
			"file_path": relPath, "relative_path": relPath, "content": chunk,
			"document_id": documentID, "document_revision": documentRevision,
			"scope_revision": scopeRevision, "chunk_ordinal": int64(ordinal),
			"content_hash": contentHash, "indexed_at": indexedAt, "modified": info.ModTime().Unix(),
			"type": "doc_chunk", "extension": strings.TrimPrefix(filepath.Ext(relPath), "."),
		}
		points[ordinal] = &qdrant.PointStruct{Id: qdrant.NewIDUUID(pointID), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{
			"": qdrant.NewVector(vector...), "sparse": qdrant.NewVectorSparse(indices, values),
		}), Payload: qdrant.NewValueMap(payload)}
	}

	if _, err := iw.QdrantClient.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: points}); err != nil {
		return fmt.Errorf("stage document revision: %w", err)
	}
	count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.CollectionName, Exact: qdrant.PtrOf(true), Filter: revisionFilter(documentID, documentRevision, scopeRevision)})
	if err != nil {
		return fmt.Errorf("verify document revision: %w", err)
	}
	if count != uint64(len(points)) {
		return fmt.Errorf("verify document revision: expected %d chunks, found %d", len(points), count)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	created := now
	if head != nil && head.CreatedAt != "" {
		created = head.CreatedAt
	}
	if err := iw.upsertControl(ctx, "document_head", documentID, map[string]any{
		"document_id": documentID, "program_id": programID, "path": relPath,
		"active_document_revision": documentRevision, "active_scope_revision": scopeRevision,
		"chunk_count": int64(len(points)), "state": "active", "created_at": created, "updated_at": now,
	}); err != nil {
		return fmt.Errorf("commit document head: %w", err)
	}

	if head != nil && (head.DocumentRevision != documentRevision || head.ScopeRevision != scopeRevision) {
		if _, err := iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(revisionFilter(documentID, head.DocumentRevision, head.ScopeRevision))}); err != nil {
			return fmt.Errorf("new revision active; stale revision cleanup pending: %w", err)
		}
	}
	return nil
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
		CreatedAt: payloadString(p, "created_at", ""),
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
				return nil, err
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
				return nil, err
			}
			cache[documentID] = head
		}
		programID := payloadString(payload, "program_id", "")
		expectedScope, checked := scopeCache[programID]
		if !checked {
			var err error
			expectedScope, err = iw.controlScopeRevision(ctx, programID)
			if err != nil {
				return nil, err
			}
			scopeCache[programID] = expectedScope
		}
		if head == nil || head.State == "deleted" || head.ProgramID != programID ||
			head.DocumentRevision != payloadString(payload, "document_revision", "") || expectedScope != payloadString(payload, "scope_revision", "") {
			continue
		}
		filtered = append(filtered, point)
	}
	return filtered, nil
}

func (iw *IngestionWorker) secureReadDocument(path string) ([]byte, string, string, os.FileInfo, error) {
	relPath, programID, err := iw.logicalDocumentPath(path)
	if err != nil {
		return nil, "", "", nil, err
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext != ".md" && ext != ".txt" && ext != ".json" {
		return nil, "", "", nil, fmt.Errorf("unsupported document extension %q", ext)
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
		return nil, "", "", nil, fmt.Errorf("document exceeds %d bytes", iw.Cfg.MaxFileSize)
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
		return nil, "", "", nil, fmt.Errorf("document exceeds %d bytes", iw.Cfg.MaxFileSize)
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, "", "", nil, errors.New("document changed while being read")
	}
	if len(content) == 0 {
		return nil, "", "", nil, errors.New("document is empty")
	}
	if !utf8.Valid(content) || isBinaryContent(content) {
		return nil, "", "", nil, errors.New("binary or non-UTF-8 document is not supported")
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
	if head.State == "deleted" || head.State == "pending_delete" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return iw.upsertControl(ctx, "document_head", documentID, map[string]any{
		"document_id": documentID, "program_id": head.ProgramID, "path": head.Path,
		"active_document_revision": head.DocumentRevision, "active_scope_revision": head.ScopeRevision,
		"chunk_count": int64(head.ChunkCount), "state": "pending_delete", "pending_since": now,
		"created_at": head.CreatedAt, "updated_at": now,
	})
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
		return err
	}
	documentID := deterministicUUID("document", iw.Cfg.HiveID, programID, rel)
	head, err := iw.readDocumentHead(ctx, documentID)
	if err != nil || head == nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := iw.upsertControl(ctx, "tombstone", documentID, map[string]any{
		"document_id": documentID, "last_document_revision": head.DocumentRevision,
		"reason": reason, "deleted_by_device_id": iw.Cfg.DeviceID, "deleted_at": now,
	}); err != nil {
		return fmt.Errorf("write tombstone: %w", err)
	}
	if err := iw.upsertControl(ctx, "document_head", documentID, map[string]any{
		"document_id": documentID, "program_id": head.ProgramID, "path": head.Path,
		"active_document_revision": head.DocumentRevision, "active_scope_revision": head.ScopeRevision,
		"chunk_count": int64(head.ChunkCount), "state": "deleted", "created_at": head.CreatedAt, "updated_at": now,
	}); err != nil {
		return fmt.Errorf("commit deleted head: %w", err)
	}
	_, err = iw.QdrantClient.Delete(ctx, &qdrant.DeletePoints{CollectionName: iw.Cfg.CollectionName, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{Must: []*qdrant.Condition{qdrant.NewMatchKeyword("document_id", documentID)}})})
	return err
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

func (iw *IngestionWorker) SyncWorkspace(ctx context.Context) (int, error) {
	if !iw.Cfg.IsWriter() {
		return 0, errors.New("workspace ingestion requires HIVE_ROLE=writer")
	}
	if err := iw.EnsureInfrastructure(ctx); err != nil {
		return 0, err
	}
	var paths []string
	err := filepath.WalkDir(iw.Cfg.DataDirectory, func(path string, entry os.DirEntry, walkErr error) error {
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
			if ext == ".md" || ext == ".txt" || ext == ".json" {
				paths = append(paths, path)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Strings(paths)
	workers := iw.Cfg.MaxEmbeddingWorkers
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan string)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if err := iw.SyncFileState(ctx, path); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("ingest %s: %w", filepath.Base(path), err)
					}
					errMu.Unlock()
				}
			}
		}()
	}
sendLoop:
	for _, path := range paths {
		select {
		case jobs <- path:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return len(paths), firstErr
	}
	return len(paths), ctx.Err()
}

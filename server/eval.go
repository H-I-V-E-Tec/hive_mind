package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Retrieval evaluation harness. It measures the current ingestion and search
// pipeline against a labelled query set: Recall@k, MRR, repetition inside the
// top-k, embedding cost and ingestion time. It never touches the private
// corpus; callers point it at a workspace they control. The harness does not
// know which embedder answers /api/embeddings, so a report only says something
// about model quality when the caller states which model produced it.

const evalReportSchemaVersion = 1

// EvalQuery is one labelled query: the relative paths under DataDirectory that
// count as relevant for it.
type EvalQuery struct {
	ID        string   `json:"id"`
	ProgramID string   `json:"program_id"`
	Query     string   `json:"query"`
	Relevant  []string `json:"relevant"`
}

// EvalQueryResult records how one query was answered.
type EvalQueryResult struct {
	ID                string   `json:"id"`
	Returned          int      `json:"returned"`
	FirstRelevantRank int      `json:"first_relevant_rank"`
	DistinctDocuments int      `json:"distinct_documents"`
	MaxDocumentShare  float64  `json:"max_document_share"`
	Paths             []string `json:"paths"`
}

// EvalRetrieval aggregates one search mode over the whole query set.
type EvalRetrieval struct {
	Mode                  string             `json:"mode"`
	Limit                 int                `json:"limit"`
	Queries               int                `json:"queries"`
	RecallAtK             map[string]float64 `json:"recall_at_k"`
	MRR                   float64            `json:"mrr"`
	MeanDistinctDocuments float64            `json:"mean_distinct_documents"`
	MeanMaxDocumentShare  float64            `json:"mean_max_document_share"`
	EmbeddingCalls        int64              `json:"embedding_calls"`
	DurationMS            int64              `json:"duration_ms"`
	Results               []EvalQueryResult  `json:"results"`
}

// EvalIngestion measures the cost of publishing the workspace once and again
// unchanged.
type EvalIngestion struct {
	Files                  int     `json:"files"`
	Chunks                 int     `json:"chunks"`
	ContentBytes           int64   `json:"content_bytes"`
	RepeatedPrefixBytes    int64   `json:"repeated_prefix_bytes"`
	RepeatedPrefixRatio    float64 `json:"repeated_prefix_ratio"`
	EmbeddingCalls         int64   `json:"embedding_calls"`
	EmbeddingPromptBytes   int64   `json:"embedding_prompt_bytes"`
	DurationMS             int64   `json:"duration_ms"`
	ReingestEmbeddingCalls int64   `json:"reingest_embedding_calls"`
	ReingestDurationMS     int64   `json:"reingest_duration_ms"`
}

// EvalReport is the JSON document produced by a full evaluation run.
type EvalReport struct {
	SchemaVersion int             `json:"schema_version"`
	Ks            []int           `json:"ks"`
	Ingestion     EvalIngestion   `json:"ingestion"`
	Retrieval     []EvalRetrieval `json:"retrieval"`
}

// LoadEvalQueries reads a labelled query file and validates it.
func LoadEvalQueries(path string) ([]EvalQuery, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var queries []EvalQuery
	if err := json.Unmarshal(raw, &queries); err != nil {
		return nil, fmt.Errorf("invalid query file: %w", err)
	}
	seen := map[string]struct{}{}
	for i, query := range queries {
		switch {
		case strings.TrimSpace(query.ID) == "":
			return nil, fmt.Errorf("query %d has no id", i)
		case strings.TrimSpace(query.ProgramID) == "":
			return nil, fmt.Errorf("query %s has no program_id", query.ID)
		case strings.TrimSpace(query.Query) == "":
			return nil, fmt.Errorf("query %s has no text", query.ID)
		case len(query.Relevant) == 0:
			return nil, fmt.Errorf("query %s has no relevant paths", query.ID)
		}
		if _, dup := seen[query.ID]; dup {
			return nil, fmt.Errorf("duplicate query id %s", query.ID)
		}
		seen[query.ID] = struct{}{}
		for _, rel := range query.Relevant {
			if !strings.HasPrefix(rel, "programs/"+query.ProgramID+"/") {
				return nil, fmt.Errorf("query %s: relevant path %q is outside programs/%s/", query.ID, rel, query.ProgramID)
			}
		}
	}
	return queries, nil
}

// RunIngestionEval publishes the workspace twice and reports what it cost.
// The second pass must find every file unchanged; it exists to show that an
// idempotent re-ingestion requests no embeddings.
func (iw *IngestionWorker) RunIngestionEval(ctx context.Context) (EvalIngestion, error) {
	var out EvalIngestion
	files, chunks, contentBytes, repeated, err := iw.evalChunkStats()
	if err != nil {
		return out, err
	}
	out.Files, out.Chunks, out.ContentBytes, out.RepeatedPrefixBytes = files, chunks, contentBytes, repeated
	if contentBytes > 0 {
		out.RepeatedPrefixRatio = float64(repeated) / float64(contentBytes)
	}

	iw.ResetEmbeddingStats()
	started := time.Now()
	first := iw.IngestWorkspaceReport(ctx, false)
	out.DurationMS = time.Since(started).Milliseconds()
	if !first.OK {
		return out, fmt.Errorf("initial ingestion failed: %s (failed=%d)", first.Error, first.Summary.Failed)
	}
	stats := iw.SnapshotEmbeddingStats()
	out.EmbeddingCalls, out.EmbeddingPromptBytes = stats.Calls, stats.PromptBytes

	iw.ResetEmbeddingStats()
	started = time.Now()
	second := iw.IngestWorkspaceReport(ctx, false)
	out.ReingestDurationMS = time.Since(started).Milliseconds()
	if !second.OK || second.Summary.Unchanged != first.Summary.Ingested {
		return out, fmt.Errorf("re-ingestion was not idempotent: unchanged=%d ingested=%d", second.Summary.Unchanged, first.Summary.Ingested)
	}
	out.ReingestEmbeddingCalls = iw.SnapshotEmbeddingStats().Calls
	return out, nil
}

// evalChunkStats parses every ingestible file exactly as SyncWorkspace does and
// measures how much of the stored text is a leading prefix shared by all chunks
// of the same document (front matter and title repeated per section).
func (iw *IngestionWorker) evalChunkStats() (files, chunks int, contentBytes, repeatedPrefixBytes int64, err error) {
	root := iw.Cfg.DataDirectory
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "programs/") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".md", ".txt", ".json":
		default:
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		parsed, parseErr := iw.parseDocument(rel, content)
		if parseErr != nil {
			return fmt.Errorf("%s: %w", rel, parseErr)
		}
		files++
		chunks += len(parsed)
		for _, chunk := range parsed {
			contentBytes += int64(len(chunk))
		}
		if len(parsed) > 1 {
			repeatedPrefixBytes += int64(len(commonPrefix(parsed))) * int64(len(parsed)-1)
		}
		return nil
	})
	return files, chunks, contentBytes, repeatedPrefixBytes, err
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		limit := len(prefix)
		if len(value) < limit {
			limit = len(value)
		}
		i := 0
		for i < limit && prefix[i] == value[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			break
		}
	}
	return prefix
}

// RunRetrievalEval answers every query with HiveSearch under the worker's
// current SearchMode and computes Recall@k for each k, MRR and top-k repetition.
func (iw *IngestionWorker) RunRetrievalEval(ctx context.Context, queries []EvalQuery, limit int, ks []int) (EvalRetrieval, error) {
	mode := strings.ToLower(strings.TrimSpace(iw.Cfg.SearchMode))
	if mode == "" {
		mode = "dense"
	}
	if len(queries) == 0 {
		return EvalRetrieval{}, errors.New("no queries to evaluate")
	}
	if len(ks) == 0 {
		return EvalRetrieval{}, errors.New("no k values to evaluate")
	}
	out := EvalRetrieval{Mode: mode, Limit: limit, Queries: len(queries), RecallAtK: map[string]float64{}, Results: []EvalQueryResult{}}
	recallSum := map[int]float64{}
	var mrrSum, distinctSum, shareSum float64

	iw.ResetEmbeddingStats()
	started := time.Now()
	for _, query := range queries {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		response, err := iw.HiveSearch(ctx, HiveSearchArguments{Query: query.Query, ProgramID: query.ProgramID, Limit: &limit})
		if err != nil {
			return out, fmt.Errorf("query %s: %w", query.ID, err)
		}
		result := scoreEvalQuery(query, response.Results, ks)
		out.Results = append(out.Results, result)
		if result.FirstRelevantRank > 0 {
			mrrSum += 1 / float64(result.FirstRelevantRank)
		}
		distinctSum += float64(result.DistinctDocuments)
		shareSum += result.MaxDocumentShare
		for _, k := range ks {
			recallSum[k] += recallAt(query, result.Paths, k)
		}
	}
	out.DurationMS = time.Since(started).Milliseconds()
	out.EmbeddingCalls = iw.SnapshotEmbeddingStats().Calls

	n := float64(len(queries))
	out.MRR = mrrSum / n
	out.MeanDistinctDocuments = distinctSum / n
	out.MeanMaxDocumentShare = shareSum / n
	for _, k := range ks {
		out.RecallAtK[strconv.Itoa(k)] = recallSum[k] / n
	}
	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].ID < out.Results[j].ID })
	return out, nil
}

func scoreEvalQuery(query EvalQuery, results []HiveSearchResult, ks []int) EvalQueryResult {
	relevant := map[string]struct{}{}
	for _, rel := range query.Relevant {
		relevant[rel] = struct{}{}
	}
	out := EvalQueryResult{ID: query.ID, Returned: len(results), Paths: []string{}}
	perDocument := map[string]int{}
	for i, result := range results {
		out.Paths = append(out.Paths, result.Path)
		perDocument[result.Path]++
		if _, ok := relevant[result.Path]; ok && out.FirstRelevantRank == 0 {
			out.FirstRelevantRank = i + 1
		}
	}
	out.DistinctDocuments = len(perDocument)
	if len(results) > 0 {
		maxCount := 0
		for _, count := range perDocument {
			if count > maxCount {
				maxCount = count
			}
		}
		out.MaxDocumentShare = float64(maxCount) / float64(len(results))
	}
	return out
}

// recallAt is the share of relevant documents that appear among the first k
// results. Results are chunk-level, so several results may name one document.
func recallAt(query EvalQuery, paths []string, k int) float64 {
	if len(query.Relevant) == 0 {
		return 0
	}
	if k > len(paths) {
		k = len(paths)
	}
	found := map[string]struct{}{}
	for _, path := range paths[:k] {
		found[path] = struct{}{}
	}
	hits := 0
	for _, rel := range query.Relevant {
		if _, ok := found[rel]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(query.Relevant))
}

// RunEval runs ingestion once and then every requested search mode, producing a
// complete report. Modes are evaluated in the given order against the same
// published index.
func (iw *IngestionWorker) RunEval(ctx context.Context, queries []EvalQuery, modes []string, limit int, ks []int) (EvalReport, error) {
	report := EvalReport{SchemaVersion: evalReportSchemaVersion, Ks: append([]int{}, ks...), Retrieval: []EvalRetrieval{}}
	ingestion, err := iw.RunIngestionEval(ctx)
	if err != nil {
		return report, err
	}
	report.Ingestion = ingestion
	original := iw.Cfg.SearchMode
	defer func() { iw.Cfg.SearchMode = original }()
	for _, mode := range modes {
		iw.Cfg.SearchMode = mode
		retrieval, err := iw.RunRetrievalEval(ctx, queries, limit, ks)
		if err != nil {
			return report, fmt.Errorf("mode %s: %w", mode, err)
		}
		report.Retrieval = append(report.Retrieval, retrieval)
	}
	return report, nil
}

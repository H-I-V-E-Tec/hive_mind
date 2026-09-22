package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var evalModes = []string{"dense", "sparse", "hybrid"}

// evalWorker builds a writer over a copy of the synthetic evaluation corpus,
// backed by the scoring in-memory Qdrant and the bag-of-words embedder.
func evalWorker(t *testing.T) (*IngestionWorker, *bagOfWordsTransport) {
	t.Helper()
	worker, root := specWorker(t, &scoringQdrant{newMemoryQdrant()})
	transport := newBagOfWordsTransport()
	worker.HTTPClient.Transport = transport
	source := filepath.Join("testdata", "eval", "corpus")
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker, transport
}

func runBaseline(t *testing.T) (EvalReport, *bagOfWordsTransport) {
	t.Helper()
	worker, transport := evalWorker(t)
	queries, err := LoadEvalQueries(filepath.Join("testdata", "eval", "queries.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.RunEval(context.Background(), queries, evalModes, defaultSearchLimit, []int{1, 3, defaultSearchLimit})
	if err != nil {
		t.Fatal(err)
	}
	return report, transport
}

func withoutTimings(report EvalReport) EvalReport {
	report.Ingestion.DurationMS = 0
	report.Ingestion.ReingestDurationMS = 0
	for i := range report.Retrieval {
		report.Retrieval[i].DurationMS = 0
	}
	return report
}

func TestRetrievalBaseline(t *testing.T) {
	report, transport := runBaseline(t)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("baseline report:\n%s", encoded)

	ing := report.Ingestion
	if ing.Files != 13 || ing.Chunks <= ing.Files {
		t.Fatalf("unexpected corpus shape: files=%d chunks=%d", ing.Files, ing.Chunks)
	}
	if ing.RepeatedPrefixBytes <= 0 || ing.RepeatedPrefixRatio <= 0 || ing.RepeatedPrefixRatio >= 1 {
		t.Fatalf("front matter repetition not measured: %+v", ing)
	}
	// Every chunk costs one embedding; infrastructure probes may add a few more.
	if ing.EmbeddingCalls < int64(ing.Chunks) || ing.EmbeddingPromptBytes < ing.ContentBytes {
		t.Fatalf("embedding cost under-counted: %+v", ing)
	}
	if ing.ReingestEmbeddingCalls != 0 {
		t.Fatalf("unchanged re-ingestion requested %d embeddings", ing.ReingestEmbeddingCalls)
	}
	if transport.calls.Load() < ing.EmbeddingCalls {
		t.Fatalf("worker counted %d embedding calls but transport served %d", ing.EmbeddingCalls, transport.calls.Load())
	}

	if len(report.Retrieval) != len(evalModes) {
		t.Fatalf("expected %d modes, got %d", len(evalModes), len(report.Retrieval))
	}
	for _, retrieval := range report.Retrieval {
		if retrieval.Queries != 11 || len(retrieval.Results) != 11 {
			t.Fatalf("%s: expected 11 queries, got %+v", retrieval.Mode, retrieval)
		}
		if retrieval.EmbeddingCalls != int64(retrieval.Queries) {
			t.Fatalf("%s: expected one embedding per query, got %d", retrieval.Mode, retrieval.EmbeddingCalls)
		}
		if retrieval.MRR <= 0 || retrieval.RecallAtK["8"] <= 0 {
			t.Fatalf("%s: harness produced no relevant hits: %+v", retrieval.Mode, retrieval)
		}
		if retrieval.RecallAtK["1"] > retrieval.RecallAtK["3"] || retrieval.RecallAtK["3"] > retrieval.RecallAtK["8"] {
			t.Fatalf("%s: recall must be monotonic in k: %v", retrieval.Mode, retrieval.RecallAtK)
		}
		for _, result := range retrieval.Results {
			if result.Returned == 0 || result.Returned > defaultSearchLimit {
				t.Fatalf("%s/%s: returned %d results", retrieval.Mode, result.ID, result.Returned)
			}
			for _, path := range result.Paths {
				if !strings.HasPrefix(path, "programs/demo/") {
					t.Fatalf("%s/%s: result outside program demo: %s", retrieval.Mode, result.ID, path)
				}
			}
		}
	}
	if strings.Contains(string(encoded), "programs/other/") {
		t.Fatal("report leaked another program's document")
	}

	if path := os.Getenv("HIVE_EVAL_WRITE"); path != "" {
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetrievalBaselineIsDeterministic(t *testing.T) {
	first, _ := runBaseline(t)
	second, _ := runBaseline(t)
	if !reflect.DeepEqual(withoutTimings(first), withoutTimings(second)) {
		a, _ := json.MarshalIndent(withoutTimings(first), "", "  ")
		b, _ := json.MarshalIndent(withoutTimings(second), "", "  ")
		t.Fatalf("baseline is not reproducible:\n%s\n---\n%s", a, b)
	}
}

func TestLoadEvalQueriesRejectsCrossProgramLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")
	if err := os.WriteFile(path, []byte(`[{"id":"q1","program_id":"demo","query":"x","relevant":["programs/other/base.md"]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEvalQueries(path); err == nil {
		t.Fatal("expected cross-program relevance label to be rejected")
	}
}

func TestEvalMetricsOnKnownRanking(t *testing.T) {
	query := EvalQuery{ID: "q", ProgramID: "demo", Query: "x", Relevant: []string{"programs/demo/a.md", "programs/demo/b.md"}}
	results := []HiveSearchResult{{Path: "programs/demo/z.md"}, {Path: "programs/demo/a.md"}, {Path: "programs/demo/a.md"}, {Path: "programs/demo/b.md"}}
	scored := scoreEvalQuery(query, results, nil)
	if scored.FirstRelevantRank != 2 || scored.DistinctDocuments != 3 || scored.MaxDocumentShare != 0.5 {
		t.Fatalf("unexpected scoring: %+v", scored)
	}
	if r := recallAt(query, scored.Paths, 1); r != 0 {
		t.Fatalf("recall@1 = %v", r)
	}
	if r := recallAt(query, scored.Paths, 3); r != 0.5 {
		t.Fatalf("recall@3 = %v", r)
	}
	if r := recallAt(query, scored.Paths, 8); r != 1 {
		t.Fatalf("recall@8 = %v", r)
	}
	if prefix := commonPrefix([]string{"---\nmeta\n---\n# T\n\nA", "---\nmeta\n---\n# T\n\nB"}); prefix != "---\nmeta\n---\n# T\n\n" {
		t.Fatalf("common prefix = %q", prefix)
	}
}

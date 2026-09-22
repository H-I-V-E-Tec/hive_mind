package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/qdrant/go-client/qdrant"
)

// scoringQdrant is memoryQdrant with a Query that actually ranks stored points:
// cosine over the unnamed dense vector, dot product over the "sparse" vector and
// reciprocal rank fusion for hybrid prefetches. It exists so the evaluation
// harness can be exercised offline with deterministic results.
type scoringQdrant struct{ *memoryQdrant }

const rrfK = 60.0

func (s *scoringQdrant) Query(_ context.Context, in *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queryCalls = append(s.queryCalls, in)
	return s.rank(in.CollectionName, in.Query, in.Using, in.Filter, in.Limit, in.Prefetch)
}

func (s *scoringQdrant) rank(collection string, query *qdrant.Query, using *string, filter *qdrant.Filter, limit *uint64, prefetch []*qdrant.PrefetchQuery) ([]*qdrant.ScoredPoint, error) {
	var scored []*qdrant.ScoredPoint
	_, isFusion := query.GetVariant().(*qdrant.Query_Fusion)
	switch {
	case isFusion && query.GetFusion() == qdrant.Fusion_RRF:
		fused := map[string]float32{}
		payloads := map[string]map[string]*qdrant.Value{}
		for _, pre := range prefetch {
			ranked, err := s.rank(collection, pre.Query, pre.Using, pre.Filter, pre.Limit, pre.Prefetch)
			if err != nil {
				return nil, err
			}
			for rank, point := range ranked {
				id := point.Id.GetUuid()
				fused[id] += float32(1 / (rrfK + float64(rank+1)))
				payloads[id] = point.Payload
			}
		}
		for id, score := range fused {
			scored = append(scored, &qdrant.ScoredPoint{Id: qdrant.NewIDUUID(id), Payload: payloads[id], Score: score})
		}
	case query.GetNearest() != nil:
		name := ""
		if using != nil {
			name = *using
		}
		for id, point := range s.points[collection] {
			if !matchesFilter(point.Payload, filter) {
				continue
			}
			stored := point.GetVectors().GetVectors().GetVectors()[name]
			if stored == nil {
				continue
			}
			var score float32
			if dense := query.GetNearest().GetDense(); dense != nil {
				score = cosine(dense.GetData(), stored.GetDense().GetData())
			} else if sparse := query.GetNearest().GetSparse(); sparse != nil {
				score = sparseDot(sparse.GetIndices(), sparse.GetValues(), stored.GetSparse().GetIndices(), stored.GetSparse().GetValues())
			} else {
				return nil, fmt.Errorf("unsupported nearest query")
			}
			scored = append(scored, &qdrant.ScoredPoint{Id: qdrant.NewIDUUID(id), Payload: point.Payload, Score: score})
		}
	default:
		return nil, fmt.Errorf("unsupported query variant")
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Id.GetUuid() < scored[j].Id.GetUuid()
	})
	if limit != nil && uint64(len(scored)) > *limit {
		scored = scored[:*limit]
	}
	return scored, nil
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func sparseDot(ai []uint32, av []float32, bi []uint32, bv []float32) float32 {
	values := map[uint32]float32{}
	for i, index := range bi {
		values[index] = bv[i]
	}
	var dot float32
	for i, index := range ai {
		dot += av[i] * values[index]
	}
	return dot
}

// bagOfWordsTransport answers Ollama's embedding API with a deterministic
// hashed bag-of-words vector. It is discriminative enough to rank fixtures but
// says nothing about a real embedding model.
type bagOfWordsTransport struct {
	dimension int
	calls     atomic.Int64
	bytes     atomic.Int64
}

const bagOfWordsDimension = 64

func newBagOfWordsTransport() *bagOfWordsTransport {
	return &bagOfWordsTransport{dimension: bagOfWordsDimension}
}

func (b *bagOfWordsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	switch req.URL.Path {
	case "/api/tags":
		body = `{"models":[{"name":"embed-model","digest":"sha256:bag-of-words-64"}]}`
	case "/api/embeddings":
		var in OllamaEmbedReq
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			return nil, err
		}
		b.calls.Add(1)
		b.bytes.Add(int64(len(in.Prompt)))
		encoded, _ := json.Marshal(OllamaEmbedResp{Embedding: bagOfWords(in.Prompt, b.dimension)})
		body = string(encoded)
	default:
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func bagOfWords(text string, dimension int) []float32 {
	vector := make([]float32, dimension)
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(token) < 3 {
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		vector[int(h.Sum32()%uint32(dimension))]++
	}
	var norm float64
	for _, v := range vector {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		vector[0] = 1
		return vector
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vector {
		vector[i] *= scale
	}
	return vector
}

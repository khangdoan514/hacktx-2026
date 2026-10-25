package context

import (
	"context"
	"strings"
	"sync"
)

// RetrievalQuery is an authorized organization retrieval.
// The resolver calls Retrieve only after the collection grant checks pass.
type RetrievalQuery struct {
	TenantID     string
	CollectionID string
	Query        string
	Limit        int
}

// Hit is one retrieved chunk. The resolver drops hits whose tenant or
// collection does not match the authorized query, including hits from a
// faulty adapter.
type Hit struct {
	TenantID     string
	CollectionID string
	DocumentID   string
	Title        string
	SourceURI    string
	Ordinal      int
	Text         string
}

// Retriever searches one organization collection.
// A fake in-memory retriever is provided; pgvector is not required.
type Retriever interface {
	Name() string
	Retrieve(ctx context.Context, query RetrievalQuery) ([]Hit, error)
}

// StaticRetriever returns chunks recorded in memory. It is the Pass 5 fake
// retrieval adapter: authorization stays in the resolver, which must not call
// Retrieve for a collection the caller cannot read.
type StaticRetriever struct {
	mu     sync.Mutex
	name   string
	chunks []Hit
	calls  []RetrievalQuery
}

// NewStaticRetriever returns a retriever containing copies of chunks.
func NewStaticRetriever(chunks ...Hit) *StaticRetriever {
	copied := make([]Hit, len(chunks))
	copy(copied, chunks)
	return &StaticRetriever{name: "static", chunks: copied}
}

// Name returns the adapter name recorded on fragment provenance.
func (s *StaticRetriever) Name() string {
	if s == nil || s.name == "" {
		return "static"
	}
	return s.name
}

// Retrieve returns hits for the query tenant and collection.
// An empty query matches every chunk in that collection, up to the limit.
func (s *StaticRetriever) Retrieve(ctx context.Context, query RetrievalQuery) ([]Hit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultRetrievalLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, query)
	var out []Hit
	needle := strings.ToLower(query.Query)
	for _, chunk := range s.chunks {
		if chunk.TenantID != query.TenantID || chunk.CollectionID != query.CollectionID {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(chunk.Text+" "+chunk.Title), needle) {
			continue
		}
		out = append(out, chunk)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Calls returns a copy of the retrieval queries received so far.
func (s *StaticRetriever) Calls() []RetrievalQuery {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RetrievalQuery, len(s.calls))
	copy(out, s.calls)
	return out
}

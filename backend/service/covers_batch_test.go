package service

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/covers"
)

// A page turn changes which tiles are on screen. PLAN §12.1 says the covers
// still being fetched for the tiles that went are work nobody will see — and,
// behind PLAN §7.4's serialising limiter, work standing in front of the covers
// that *are* on screen. So a batch supersedes the last one outright.
func TestCoverBatchCancelsThePreviousOne(t *testing.T) {
	// covers is only checked for nil here; nothing is fetched, because the
	// batches below are empty. What is under test is the handover.
	s := &Service{covers: &covers.Cache{}}

	s.runCoverBatch(context.Background(), nil, "src", nil)
	first := s.coverBatch
	if first == nil {
		t.Fatal("the first batch left no context to cancel")
	}
	select {
	case <-first.Done():
		t.Fatal("the first batch was cancelled before a second one arrived")
	default:
	}

	s.runCoverBatch(context.Background(), nil, "src", nil)
	select {
	case <-first.Done():
	default:
		t.Fatal("a new batch did not cancel the covers still in flight for the old page")
	}

	if s.coverBatch == first {
		t.Fatal("the second batch reused the cancelled context")
	}
}

// Without a cover cache there is nothing to fetch and nothing to cancel; a
// frontend that asks anyway must not be a nil dereference.
func TestCoverBatchWithoutACacheDoesNothing(t *testing.T) {
	s := &Service{}
	s.runCoverBatch(context.Background(), nil, "src", []coverWant{{SeriesID: "a", URL: "https://example.invalid/a.jpg"}})
	if s.coverBatch != nil {
		t.Fatal("a service with no cover cache started a batch")
	}
}

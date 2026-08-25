package worker

import (
	"strconv"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/xfeed"
)

func TestRequestXStatusEnrichmentDeduplicatesWithinWindow(t *testing.T) {
	m := &Manager{
		xStatusEnrich: make(chan xfeed.StatusEnrichmentRequest, 2),
		xStatusQueued: newXStatusEnrichmentCache(),
	}
	req := xfeed.StatusEnrichmentRequest{
		Kind: xfeed.StatusEnrichmentMissingQuoteParent,
		Ref:  xfeed.StatusRef{Handle: "sample_user", TweetID: "1000000000000000001"},
	}

	m.RequestXStatusEnrichment(req)
	m.RequestXStatusEnrichment(req)

	if got := len(m.xStatusEnrich); got != 1 {
		t.Fatalf("queued requests = %d, want 1", got)
	}
}

func TestRequestXStatusEnrichmentAcceptsExpiredDedupEntry(t *testing.T) {
	m := &Manager{
		xStatusEnrich: make(chan xfeed.StatusEnrichmentRequest, 1),
		xStatusQueued: newXStatusEnrichmentCache(),
	}
	req := xfeed.StatusEnrichmentRequest{
		Kind: xfeed.StatusEnrichmentMissingQuoteParent,
		Ref:  xfeed.StatusRef{Handle: "sample_user", TweetID: "1000000000000000001"},
	}
	m.xStatusQueued.Add(xStatusEnrichmentKey(req), time.Now().Add(-xStatusEnrichmentDedupWindow))

	m.RequestXStatusEnrichment(req)

	if got := len(m.xStatusEnrich); got != 1 {
		t.Fatalf("queued requests = %d, want expired key to be accepted", got)
	}
}

func TestRequestXStatusEnrichmentDedupCacheStaysBounded(t *testing.T) {
	m := &Manager{
		xStatusEnrich: make(chan xfeed.StatusEnrichmentRequest, xStatusEnrichmentDedupCapacity+2),
		xStatusQueued: newXStatusEnrichmentCache(),
	}
	request := func(tweetID string) {
		m.RequestXStatusEnrichment(xfeed.StatusEnrichmentRequest{
			Kind: xfeed.StatusEnrichmentMissingQuoteParent,
			Ref:  xfeed.StatusRef{Handle: "sample_user", TweetID: tweetID},
		})
	}

	for i := 1; i <= xStatusEnrichmentDedupCapacity+1; i++ {
		request(strconv.Itoa(i))
	}

	if got := m.xStatusQueued.Len(); got != xStatusEnrichmentDedupCapacity {
		t.Fatalf("dedup cache entries = %d, want %d", got, xStatusEnrichmentDedupCapacity)
	}

	request("1")
	if got := len(m.xStatusEnrich); got != xStatusEnrichmentDedupCapacity+2 {
		t.Fatalf("queued requests = %d, want evicted oldest key to be accepted", got)
	}
}

func TestRequestXStatusEnrichmentForgetsDroppedRequest(t *testing.T) {
	m := &Manager{
		xStatusEnrich: make(chan xfeed.StatusEnrichmentRequest),
		xStatusQueued: newXStatusEnrichmentCache(),
	}
	req := xfeed.StatusEnrichmentRequest{
		Kind: xfeed.StatusEnrichmentMissingQuoteParent,
		Ref:  xfeed.StatusRef{Handle: "sample_user", TweetID: "1000000000000000001"},
	}

	m.RequestXStatusEnrichment(req)

	if got := m.xStatusQueued.Len(); got != 0 {
		t.Fatalf("dedup cache entries after queue rejection = %d, want 0", got)
	}
}

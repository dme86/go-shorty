package httpx

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/example/shorty/internal/meta"
	"github.com/example/shorty/internal/store"
)

type metadataJob struct {
	tenantID string
	code     string
	linkURL  string
}

type metadataQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	jobs   chan metadataJob
}

// A fixed worker pool with a bounded buffer prevents unbounded goroutines
// when users create links faster than external sites can be scraped.
func newMetadataQueue(workers, capacity int, process func(context.Context, metadataJob)) *metadataQueue {
	if workers < 1 {
		workers = 4
	}
	if capacity < 1 {
		capacity = 64
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &metadataQueue{ctx: ctx, cancel: cancel, done: make(chan struct{}), jobs: make(chan metadataJob, capacity)}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-q.jobs:
					if ctx.Err() == nil {
						process(ctx, job)
					}
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(q.done)
	}()
	return q
}

func (q *metadataQueue) submit(job metadataJob) bool {
	if q == nil || q.ctx.Err() != nil {
		return false
	}
	select {
	case <-q.ctx.Done():
		return false
	case q.jobs <- job:
		return true
	default:
		return false
	}
}

func (q *metadataQueue) Close() {
	if q != nil {
		q.cancel()
		<-q.done
	}
}

func metadataProcessor(st store.Store) func(context.Context, metadataJob) {
	return func(ctx context.Context, job metadataJob) {
		jobCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		md, err := meta.Fetch(jobCtx, job.linkURL, 5*time.Second)
		if err != nil {
			log.Printf("metadata fetch failed for code=%s: %v", job.code, err)
			return
		}
		md.Tags = meta.InferTags(job.linkURL, md)
		if err := st.UpdateMeta(jobCtx, job.tenantID, job.code, md); err != nil {
			log.Printf("metadata update failed for code=%s: %v", job.code, err)
		}
	}
}

package httpx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetadataQueueCapacityAndShutdown(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var processed atomic.Int32
	q := newMetadataQueue(1,1,func(ctx context.Context,_ metadataJob) {
		processed.Add(1)
		select {case started<-struct{}{}: default:}
		select {case <-release: case <-ctx.Done():}
	})
	if !q.submit(metadataJob{code:"first"}) {t.Fatal("first job was rejected")}
	select {case <-started: case <-time.After(time.Second):t.Fatal("worker did not start")}
	if !q.submit(metadataJob{code:"second"}) {t.Fatal("buffered job was rejected")}
	if q.submit(metadataJob{code:"third"}) {t.Fatal("queue accepted work beyond its capacity")}
	q.Close()
	close(release)
	if q.submit(metadataJob{code:"later"}) {t.Fatal("stopped queue accepted work")}
	if got:=processed.Load();got!=1 {t.Fatalf("processed=%d, want 1",got)}
}

func TestMetadataQueueNeverExceedsWorkerLimit(t *testing.T) {
	started := make(chan struct{},2)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	q:=newMetadataQueue(2,2,func(ctx context.Context,_ metadataJob) {
		a:=active.Add(1)
		for {
			old:=maxActive.Load()
			if a<=old||maxActive.CompareAndSwap(old,a){break}
		}
		started<-struct{}{}
		select {case <-release: case <-ctx.Done():}
		active.Add(-1)
	})
	for i:=0;i<2;i++ {if !q.submit(metadataJob{}) {t.Fatal("failed to enqueue")}}
	for i:=0;i<2;i++ {select {case <-started:case <-time.After(time.Second):t.Fatal("worker did not run")}}
	if got:=maxActive.Load();got!=2 {t.Fatalf("max concurrency=%d, want 2",got)}
	close(release)
	q.Close()
}

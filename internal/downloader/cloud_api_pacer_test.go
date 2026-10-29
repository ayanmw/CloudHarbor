package downloader

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestCloudAPIPacerSharesSpacingAcrossConcurrentCallers(t *testing.T) {
	pacer := newCloudAPIPacer(35 * time.Millisecond)
	const callers = 6
	times := make([]time.Time, callers)
	var wg sync.WaitGroup
	for i := range times {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := pacer.Wait(context.Background()); err != nil {
				t.Errorf("wait: %v", err)
				return
			}
			times[i] = time.Now()
		}(i)
	}
	wg.Wait()
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 25*time.Millisecond {
			t.Fatalf("shared request slots overlapped: gap %v between %s and %s", gap, times[i-1], times[i])
		}
	}
}

func TestCloudAPIPacerHonorsCancellationWhileWaiting(t *testing.T) {
	pacer := newCloudAPIPacer(100 * time.Millisecond)
	if err := pacer.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := pacer.Wait(ctx); err == nil {
		t.Fatal("cancelled request waited through its reserved delay")
	}
}

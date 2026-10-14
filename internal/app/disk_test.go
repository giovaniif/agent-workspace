package app_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
)

type gatedSizer struct {
	release chan struct{}
	sizes   map[string]int64
	err     error

	mu        sync.Mutex
	calls     map[string]int
	running   int
	maxAtOnce int
}

func newGatedSizer(sizes map[string]int64) *gatedSizer {
	return &gatedSizer{release: make(chan struct{}), sizes: sizes, calls: map[string]int{}}
}

func (g *gatedSizer) Size(_ context.Context, path string) (int64, error) {
	g.mu.Lock()
	g.calls[path]++
	g.running++
	g.maxAtOnce = max(g.maxAtOnce, g.running)
	g.mu.Unlock()
	<-g.release
	g.mu.Lock()
	g.running--
	g.mu.Unlock()
	if g.err != nil {
		return 0, g.err
	}
	return g.sizes[path], nil
}

func (g *gatedSizer) callsFor(path string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls[path]
}

type stepClock struct{ now atomic.Int64 }

func (c *stepClock) Now() time.Time          { return time.Unix(c.now.Load(), 0) }
func (c *stepClock) advance(d time.Duration) { c.now.Add(int64(d / time.Second)) }

func TestDiskSizesNeverBlockOnTheSizer(t *testing.T) {
	sizer := newGatedSizer(map[string]int64{"/w/a": 500})
	d := app.NewDiskSizes(sizer, 2, time.Minute, time.Now)

	done := make(chan struct{})
	go func() {
		_, known := d.Get("/w/a")
		if known {
			t.Error("Get reported a size before the sizer answered")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Get blocked on the sizer")
	}

	close(sizer.release)
	d.Wait()
	if size, known := d.Get("/w/a"); !known || size != 500 {
		t.Errorf("Get after the sizer answered = %d, %v; want 500, true", size, known)
	}
}

func TestDiskSizesRunAtMostWorkersAtOnceAndMeasureEachPathOnce(t *testing.T) {
	sizes := map[string]int64{}
	paths := []string{"/w/a", "/w/b", "/w/c", "/w/d", "/w/e"}
	for i, p := range paths {
		sizes[p] = int64(i + 1)
	}
	sizer := newGatedSizer(sizes)
	d := app.NewDiskSizes(sizer, 2, time.Minute, time.Now)
	for range 3 {
		for _, p := range paths {
			d.Get(p)
		}
	}
	time.Sleep(50 * time.Millisecond)
	close(sizer.release)
	d.Wait()

	if sizer.maxAtOnce > 2 {
		t.Errorf("%d sizes ran at once, want at most 2", sizer.maxAtOnce)
	}
	for _, p := range paths {
		if n := sizer.callsFor(p); n != 1 {
			t.Errorf("%s measured %d times, want once", p, n)
		}
	}
}

func TestDiskSizesKeepShowingTheOldSizeWhileAStaleOneIsRemeasured(t *testing.T) {
	clock := &stepClock{}
	clock.advance(time.Hour)
	sizer := newGatedSizer(map[string]int64{"/w/a": 500})
	close(sizer.release)
	d := app.NewDiskSizes(sizer, 2, time.Minute, clock.Now)
	d.Get("/w/a")
	d.Wait()

	d.Get("/w/a")
	d.Wait()
	if n := sizer.callsFor("/w/a"); n != 1 {
		t.Fatalf("a fresh size was measured %d times, want once", n)
	}

	sizer.sizes["/w/a"] = 900
	clock.advance(2 * time.Minute)
	if size, known := d.Get("/w/a"); !known || size != 500 {
		t.Errorf("stale Get = %d, %v; want the old 500 while remeasuring", size, known)
	}
	d.Wait()
	if size, known := d.Get("/w/a"); !known || size != 900 {
		t.Errorf("Get after remeasuring = %d, %v; want 900", size, known)
	}
}

func TestDiskSizesForgetDropsTheSizeSoAChangedWorktreeIsMeasuredAgain(t *testing.T) {
	sizer := newGatedSizer(map[string]int64{"/w/a": 500})
	close(sizer.release)
	d := app.NewDiskSizes(sizer, 2, time.Hour, time.Now)
	d.Get("/w/a")
	d.Wait()

	sizer.sizes["/w/a"] = 10
	d.Forget("/w/a")
	if _, known := d.Get("/w/a"); known {
		t.Error("Get after Forget still knew a size")
	}
	d.Wait()
	if size, _ := d.Get("/w/a"); size != 10 {
		t.Errorf("size after Forget = %d, want the new 10", size)
	}
}

func TestDiskSizesRetryAFailedMeasurementOnceItIsStale(t *testing.T) {
	clock := &stepClock{}
	clock.advance(time.Hour)
	sizer := newGatedSizer(map[string]int64{"/w/a": 500})
	sizer.err = errors.New("du failed")
	close(sizer.release)
	d := app.NewDiskSizes(sizer, 2, time.Minute, clock.Now)
	d.Get("/w/a")
	d.Wait()
	if _, known := d.Get("/w/a"); known {
		t.Error("a failed measurement was reported as a size")
	}
	d.Wait()
	if n := sizer.callsFor("/w/a"); n != 1 {
		t.Errorf("failure retried %d times before it was stale, want no retry", n-1)
	}

	sizer.err = nil
	clock.advance(2 * time.Minute)
	d.Get("/w/a")
	d.Wait()
	if size, known := d.Get("/w/a"); !known || size != 500 {
		t.Errorf("Get after the retry = %d, %v; want 500, true", size, known)
	}
}

func TestDiskSizesForgetDuringAMeasurementDoesNotTrustItsResult(t *testing.T) {
	sizer := newGatedSizer(map[string]int64{"/w/a": 500})
	d := app.NewDiskSizes(sizer, 2, time.Hour, time.Now)
	d.Get("/w/a")
	time.Sleep(20 * time.Millisecond)

	d.Forget("/w/a")
	sizer.mu.Lock()
	sizer.sizes["/w/a"] = 10
	sizer.mu.Unlock()
	close(sizer.release)
	d.Wait()

	d.Get("/w/a")
	d.Wait()
	if size, _ := d.Get("/w/a"); size != 10 {
		t.Errorf("size = %d, want the 10 measured after the change", size)
	}
}

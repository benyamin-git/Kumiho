package logging

import (
	"sync"
	"testing"
)

// TestSanitizerSwapWhileLoggingIsRaceFree logs while the runtime setting swaps
// the sanitizer; run under -race to catch an unsynchronized read.
func TestSanitizerSwapWhileLoggingIsRaceFree(t *testing.T) {
	r := NewRing(Options{Capacity: 16})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r.Log(Info, "test", "dialing 203.0.113.7")
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			r.SetSanitize(Sanitizer(true))
		} else {
			r.SetSanitize(nil)
		}
	}
	close(stop)
	wg.Wait()
}

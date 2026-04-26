package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080/users", "target URL")
	concurrency := flag.Int("c", 50, "concurrent workers")
	duration := flag.Duration("d", 10*time.Second, "test duration")
	flag.Parse()

	var (
		total   int64
		success int64
		failed  int64
	)

	client := &http.Client{Timeout: 5 * time.Second}

	fmt.Printf("Load test: %s\n", *url)
	fmt.Printf("Workers: %d | Duration: %s\n\n", *concurrency, *duration)

	start := time.Now()
	done := make(chan struct{})
	time.AfterFunc(*duration, func() { close(done) })

	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					resp, err := client.Get(*url)
					atomic.AddInt64(&total, 1)
					if err != nil || resp.StatusCode != http.StatusOK {
						atomic.AddInt64(&failed, 1)
					} else {
						atomic.AddInt64(&success, 1)
					}
					if resp != nil {
						resp.Body.Close()
					}
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	rps := float64(total) / elapsed.Seconds()
	fmt.Printf("Duration:  %.1fs\n", elapsed.Seconds())
	fmt.Printf("Requests:  %d total\n", total)
	fmt.Printf("Success:   %d\n", success)
	fmt.Printf("Failed:    %d\n", failed)
	fmt.Printf("RPS:       %.0f req/s\n", rps)
}

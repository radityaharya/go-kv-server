package main

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	TotalRequests uint64
	Errors        uint64
	LatencySum    uint64
}

type KeyValueRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	cryptorand.Read(b)
	return b
}

func Worker(host string, metrics *Metrics, wg *sync.WaitGroup, duration time.Duration, burstMode bool) {
	defer wg.Done()
	client := &http.Client{Timeout: 2 * time.Second}
	endTime := time.Now().Add(duration)

	for time.Now().Before(endTime) {
		start := time.Now()
		var err error

		if mathrand.Float32() < 0.5 {
			key := fmt.Sprintf("key%d", mathrand.Intn(10000))
			_, err = client.Get(fmt.Sprintf("%s/api/ns/benchmark/get/%s", host, key))
		} else {
			key := fmt.Sprintf("key%d", mathrand.Intn(10000))
			payloadSize := mathrand.Intn(99*1024) + 1024
			value := generateRandomBytes(payloadSize)
			data := KeyValueRequest{
				Key:   key,
				Value: string(value),
			}
			jsonData, _ := json.Marshal(data)

			_, err = client.Post(
				fmt.Sprintf("%s/api/ns/benchmark/set", host),
				"application/json",
				bytes.NewBuffer(jsonData),
			)
		}

		latency := time.Since(start).Microseconds()
		atomic.AddUint64(&metrics.LatencySum, uint64(latency))
		atomic.AddUint64(&metrics.TotalRequests, 1)

		if err != nil {
			atomic.AddUint64(&metrics.Errors, 1)
		}

		if !burstMode {
			time.Sleep(time.Duration(mathrand.Intn(10)) * time.Millisecond)
		}
	}
}

func CreateBenchmarkNamespace(host string) error {
	resp, err := http.Post(fmt.Sprintf("%s/api/namespace/benchmark", host), "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func main() {
	mathrand.Seed(time.Now().UnixNano())

	host := flag.String("host", "http://localhost:8080", "server host URL")
	workers := flag.Int("workers", 10, "number of concurrent workers")
	duration := flag.Duration("duration", 30*time.Second, "test duration")
	burst := flag.Bool("burst", false, "enable burst mode (no delays between requests)")
	flag.Parse()

	if err := CreateBenchmarkNamespace(*host); err != nil {
		fmt.Printf("Failed to create benchmark namespace: %v\n", err)
		return
	}

	metrics := &Metrics{}
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go Worker(*host, metrics, &wg, *duration, *burst)
	}

	ticker := time.NewTicker(time.Second)
	go func() {
		for range ticker.C {
			fmt.Printf("\rRequests: %d, Errors: %d", metrics.TotalRequests, metrics.Errors)
		}
	}()

	wg.Wait()
	ticker.Stop()
	elapsed := time.Since(start)

	fmt.Printf("\n\nBenchmark Results:\n")
	fmt.Printf("Duration: %v\n", elapsed)
	fmt.Printf("Total Requests: %d\n", metrics.TotalRequests)
	fmt.Printf("Requests/sec: %.2f\n", float64(metrics.TotalRequests)/elapsed.Seconds())
	fmt.Printf("Errors: %d\n", metrics.Errors)
	fmt.Printf("Average Latency: %.2f ms\n", float64(metrics.LatencySum)/float64(metrics.TotalRequests)/1000)
}

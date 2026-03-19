package balancer

import (
	"sync"
	"sync/atomic"

	"intelligent-lb/internal/metrics"
)

// Canary implements fixed-percentage traffic splitting based on configured weights.
// Unlike WeightedScore which uses dynamic latency/load scoring, Canary treats
// weights as absolute traffic percentages — a server with weight 90 will
// receive exactly ~90% of traffic regardless of performance metrics.
//
// This mirrors Traefik's weighted round-robin for canary deployments,
// using the Smooth Weighted Round Robin (SWRR) algorithm from NGINX.
type Canary struct {
	mu             sync.Mutex
	serverWeights  map[string]int // configured weights per URL
	currentWeights map[string]int // running SWRR current weights
	initialized    atomic.Bool
}

// initWeights sets up the SWRR state from the metrics snapshot.
// Called once on first Select().
func (c *Canary) initWeights(stats map[string]metrics.ServerStats) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized.Load() {
		return
	}

	c.serverWeights = make(map[string]int)
	c.currentWeights = make(map[string]int)

	for url, s := range stats {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		c.serverWeights[url] = w
		c.currentWeights[url] = 0
	}
	c.initialized.Store(true)
}

// Select picks the next server using Smooth Weighted Round Robin.
// Each call:
//  1. Add effective weight to each server's current weight
//  2. Pick the server with the highest current weight
//  3. Subtract total weight from the chosen server's current weight
//
// This guarantees smooth distribution matching the configured percentages.
func (c *Canary) Select(
	candidates []string,
	stats map[string]metrics.ServerStats,
	priority string,
) string {
	if len(candidates) == 0 {
		return ""
	}

	if !c.initialized.Load() {
		c.initWeights(stats)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build candidate set for quick lookup
	candidateSet := make(map[string]bool, len(candidates))
	for _, url := range candidates {
		candidateSet[url] = true
	}

	// Calculate total weight of active candidates
	totalWeight := 0
	for _, url := range candidates {
		w, ok := c.serverWeights[url]
		if !ok {
			w = 1
			c.serverWeights[url] = w
			c.currentWeights[url] = 0
		}
		totalWeight += w
	}

	if totalWeight == 0 {
		return candidates[0]
	}

	// SWRR: increment current weights and find the maximum
	best := ""
	bestCW := -1 << 30 // very small number

	for _, url := range candidates {
		ew := c.serverWeights[url]
		c.currentWeights[url] += ew

		if c.currentWeights[url] > bestCW {
			bestCW = c.currentWeights[url]
			best = url
		}
	}

	// Subtract total from the chosen server
	if best != "" {
		c.currentWeights[best] -= totalWeight
	}

	return best
}

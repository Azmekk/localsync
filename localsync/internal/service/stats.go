package service

import (
	"fmt"
	"log"
	"time"
)

// StartStatsDisplay starts a goroutine that periodically logs client stats.
// It stops when the done channel is closed.
func StartStatsDisplay(hub *Hub, interval time.Duration, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				displayStats(hub)
			}
		}
	}()
}

func displayStats(hub *Hub) {
	stats := hub.GetClientStats()
	if len(stats) == 0 {
		return
	}

	cutoff := time.Now().Add(-5 * time.Second)
	var active int
	for _, s := range stats {
		if !s.LastUpdate.IsZero() && s.LastUpdate.After(cutoff) {
			active++
		}
	}
	if active == 0 {
		return
	}

	log.Println("--- Client Stats ---")
	for _, s := range stats {
		if s.LastUpdate.IsZero() || s.LastUpdate.Before(cutoff) {
			continue
		}
		name := s.Name
		if name == "" {
			name = "unknown"
		}
		minutes := int(s.Pos) / 60
		seconds := s.Pos - float64(minutes*60)
		log.Printf("  %s (%s):", name, s.IP)
		log.Printf("    Speed: %.0f kbps | Buffer: %.1fs | Pos: %02d:%04.1f",
			s.SpeedKbps, s.BufferSecs, minutes, seconds)
	}
	fmt.Println("--------------------")
}

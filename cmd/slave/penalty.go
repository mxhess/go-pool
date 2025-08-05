

// Add to cmd/slave/penalty.go

package main

import (
	"sync"
	"time"
	"go-pool/logger"
	"go-pool/config"
)

type ShareWindow struct {
	Good           int
	Bad            int
	StartTime      time.Time
	LastShareTime  time.Time
	ShareTimings   []time.Time // Track last N share timestamps for rate limiting
}

type PenaltyBox struct {
	mu       sync.RWMutex
	banned   map[string]time.Time    // IP -> ban expiry time
	windows  map[uint64]*ShareWindow // ConnID -> share window
	ipConns  map[string][]uint64     // IP -> list of ConnIDs
}

const (
	MaxSharesPerSecond = 10  // Maximum shares per second per connection
	ShareTimingWindow  = 100 // Track last 100 shares for rate calculation
)

var penaltyBox = &PenaltyBox{
	banned:  make(map[string]time.Time),
	windows: make(map[uint64]*ShareWindow),
	ipConns: make(map[string][]uint64),
}

// RecordShare tracks share submission for penalty calculation and rate limiting
func (pb *PenaltyBox) RecordShare(connID uint64, ip string, isValid bool) bool {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	now := time.Now()

	// Check if IP is banned
	if banTime, exists := pb.banned[ip]; exists {
		if now.Before(banTime) {
			return false // Still banned
		}
		// Ban expired, remove it
		delete(pb.banned, ip)
	}

	// Get or create window for this connection
	window, exists := pb.windows[connID]
	if !exists || time.Since(window.StartTime) > time.Duration(config.Cfg.SlaveConfig.ShareWindowMinutes)*time.Minute {
		// New window or expired window
		window = &ShareWindow{
			StartTime:    now,
			ShareTimings: make([]time.Time, 0, config.Cfg.SlaveConfig.ShareTimingWindowSize),
		}
		pb.windows[connID] = window
		
		// Track IP->connection mapping
		if !contains(pb.ipConns[ip], connID) {
			pb.ipConns[ip] = append(pb.ipConns[ip], connID)
		}
	}

	// Rate limiting check
	window.ShareTimings = append(window.ShareTimings, now)
	if len(window.ShareTimings) > config.Cfg.SlaveConfig.ShareTimingWindowSize {
		window.ShareTimings = window.ShareTimings[1:] // Remove oldest
	}

	// Calculate shares per second over recent window
	if len(window.ShareTimings) >= 10 { // Need at least 10 shares to calculate rate
		windowDuration := now.Sub(window.ShareTimings[0]).Seconds()
		if windowDuration > 0 {
			sharesPerSecond := float64(len(window.ShareTimings)) / windowDuration
			
			if sharesPerSecond > config.Cfg.SlaveConfig.MaxSharesPerSecond {
				// Rate limit exceeded - ban for share spam
				banDuration := time.Duration(config.Cfg.SlaveConfig.BanDurationMinutes) * time.Minute
				pb.banned[ip] = now.Add(banDuration)
				
				// Clear windows for all connections from this IP
				for _, cid := range pb.ipConns[ip] {
					delete(pb.windows, cid)
				}
				
				logger.Warn("Penalty box: Banned IP", ip, "for share spam. Rate:", sharesPerSecond, "shares/sec")
				return false
			}
		}
	}

	// Update counts for bad share ratio
	if isValid {
		window.Good++
	} else {
		window.Bad++
	}

	// Check if we should ban (configurable bad share ratio)
	total := window.Good + window.Bad
	if total >= 6 && window.Bad > 0 {
		ratio := float64(window.Bad) / float64(total)
		if ratio > config.Cfg.SlaveConfig.BadShareRatio {
			// Ban the IP
			banDuration := time.Duration(config.Cfg.SlaveConfig.BanDurationMinutes) * time.Minute
			pb.banned[ip] = now.Add(banDuration)
			
			// Clear windows for all connections from this IP
			for _, cid := range pb.ipConns[ip] {
				delete(pb.windows, cid)
			}
			
			logger.Warn("Penalty box: Banned IP", ip, "for excessive bad shares. Ratio:", ratio)
			return false
		}
	}

	window.LastShareTime = now
	return true
}

// CheckBanned checks if an IP is currently banned
func (pb *PenaltyBox) CheckBanned(ip string) bool {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	if banTime, exists := pb.banned[ip]; exists {
		if time.Now().Before(banTime) {
			return true
		}
	}
	return false
}

// CleanupConnection removes tracking for a disconnected client
func (pb *PenaltyBox) CleanupConnection(connID uint64, ip string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	delete(pb.windows, connID)
	
	// Remove from IP mapping
	if conns, exists := pb.ipConns[ip]; exists {
		newConns := make([]uint64, 0, len(conns)-1)
		for _, cid := range conns {
			if cid != connID {
				newConns = append(newConns, cid)
			}
		}
		if len(newConns) == 0 {
			delete(pb.ipConns, ip)
		} else {
			pb.ipConns[ip] = newConns
		}
	}
}

// Periodic cleanup of old data
func (pb *PenaltyBox) StartCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			pb.mu.Lock()
			
			// Clean expired windows
			for connID, window := range pb.windows {
				if time.Since(window.StartTime) > 10*time.Minute {
					delete(pb.windows, connID)
				}
			}
			
			// Clean expired bans
			for ip, banTime := range pb.banned {
				if time.Now().After(banTime) {
					delete(pb.banned, ip)
				}
			}
			
			pb.mu.Unlock()
		}
	}()
}

// GetStats returns current penalty box statistics
func (pb *PenaltyBox) GetStats() map[string]interface{} {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	stats := map[string]interface{}{
		"banned_ips":        len(pb.banned),
		"active_windows":    len(pb.windows),
		"tracked_ips":       len(pb.ipConns),
	}

	// Get list of currently banned IPs
	bannedList := make([]string, 0, len(pb.banned))
	for ip, expiry := range pb.banned {
		if time.Now().Before(expiry) {
			bannedList = append(bannedList, ip)
		}
	}
	stats["banned_list"] = bannedList

	return stats
}

func contains(slice []uint64, val uint64) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}


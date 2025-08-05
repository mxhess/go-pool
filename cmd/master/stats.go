/*
 * This file is part of go-pool.
 *
 * go-pool is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * go-pool is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with go-pool. If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
    "go-pool/logger"
    "math"
    "sync"
    "time"
    "github.com/mxhess/go-salvium/rpc/wallet"
)

// Keep these types for API compatibility
type LastBlock struct {
    Height    uint64 `json:"height"`
    Timestamp int64  `json:"timestamp"`
    Reward    uint64 `json:"reward"`
    Hash      string `json:"hash"`
}

type Hr struct {
    Time     int64   `json:"t"`
    Hashrate float64 `json:"h"`
}

type FoundInfo struct {
    Height uint64 `json:"height"`
    Hash   string `json:"hash"`
}

type Withdrawal struct {
    Txid         string `json:"txid"`
    Timestamp    uint64 `json:"time"`
    Destinations []wallet.Destination
}

// Minimal Statistics struct - only keep what's absolutely needed
type Statistics struct {
    // These are only used temporarily until we remove all references
    PoolHashrate      float64
    NetHashrate       float64
    LastBlock         LastBlock
    RecentWithdrawals []Withdrawal
    
    // This lock is still needed for the few remaining uses
    sync.RWMutex
}

// Global Stats variable - will be removed eventually
var Stats = Statistics{}

// Get5mHashrate now queries SQLite instead of in-memory shares
func Get5mHashrate(wallet string) float64 {
    windowStart := time.Now().Unix() - 300 // 5 minutes
    shares, err := Ledger.GetMinerSharesInWindow(wallet, windowStart)
    if err != nil {
        logger.Warn("Failed to get 5m hashrate:", err)
        return 0
    }
    
    var totalDiff uint64
    for _, share := range shares {
        totalDiff += share.Difficulty
    }
    
    return math.Round(float64(totalDiff) / 300.0)
}

// Get15mHashrate now queries SQLite
func Get15mHashrate(wallet string) float64 {
    windowStart := time.Now().Unix() - 900 // 15 minutes
    shares, err := Ledger.GetMinerSharesInWindow(wallet, windowStart)
    if err != nil {
        logger.Warn("Failed to get 15m hashrate:", err)
        return 0
    }
    
    var totalDiff uint64
    for _, share := range shares {
        totalDiff += share.Difficulty
    }
    
    return math.Round(float64(totalDiff) / 900.0)
}

// Helper to sanitize float values
func sanitizeFloat(val float64) float64 {
    if math.IsNaN(val) || math.IsInf(val, 0) {
        return 0
    }
    return val
}


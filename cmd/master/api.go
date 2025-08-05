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
	"encoding/json"
	"fmt"
	"go-pool/config"
	"go-pool/logger"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var Coin = math.Pow10(config.Cfg.Atomic)

func Round0(val float64) float64 {
	return math.Round(val)
}

func Round3(val float64) float64 {
	return math.Round(val*1000) / 1000
}

func Round6(val float64) float64 {
	return math.Round(val*1000000) / 1000000
}

func NotNan(val float64) float64 {
	if math.IsNaN(val) {
		return 0
	}
	return val
}

// ✅ PURE SQLITE MINER STATS - NO BOLT GARBAGE!
func GetMinerStats(address string) (map[string]interface{}, error) {
	logger.Debug("📊 Getting miner stats via SQLite ledger for:", address)

	// Get balance from SQLite ledger
	balance, err := Ledger.GetBalance(address)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Get recent performance data
	// TODO: Implement hashrate calculation from shares

	stats := map[string]interface{}{
		"address":         address,
		"balance":         NotNan(Round6(float64(balance.BalanceConfirmed) / Coin)),
		"balance_pending": NotNan(Round6(float64(balance.BalancePending) / Coin)),
		"paid":           NotNan(Round6(float64(balance.TotalPaid) / Coin)),
		"hashrate_5m":    0,    // TODO: Calculate from recent shares
		"hashrate_15m":   0,    // TODO: Calculate from recent shares  
		"hashrate_1h":    0,    // TODO: Calculate from recent shares
		"worker_count":   0,    // TODO: Get from workers table
		"last_seen":      balance.LastUpdated,
		"withdrawals":    []interface{}{}, // TODO: Get recent withdrawals
	}

	logger.Debug("✅ Miner stats retrieved from SQLite ledger")
	return stats, nil
}

// ✅ PURE SQLITE POOL STATS
func GetPoolStats() (map[string]interface{}, error) {
	logger.Debug("📊 Getting pool stats via SQLite ledger")

	// Get recent blocks
	blocks, err := Ledger.GetRecentBlocks(10)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blocks: %w", err)
	}

	// Get top miners
	topMiners, err := Ledger.GetTopMiners(10)
	if err != nil {
		return nil, fmt.Errorf("failed to get top miners: %w", err)
	}

	// TODO: Calculate pool hashrate from recent shares
	// TODO: Get connected miners/workers count

	stats := map[string]interface{}{
		"pool_hashrate":    0,    // TODO: Calculate from shares
		"connected_miners": 0,    // TODO: Get from workers table
		"connected_workers": 0,   // TODO: Get from workers table
		"blocks_found":     len(blocks),
		"recent_blocks":    blocks,
		"top_miners":       topMiners,
		"pool_fee_percent": config.Cfg.MasterConfig.FeePercent,
		"min_payout":       config.Cfg.MasterConfig.MinWithdrawal,
		"last_update":      time.Now().Unix(),
	}

	logger.Debug("✅ Pool stats retrieved from SQLite ledger")
	return stats, nil
}

// ✅ CLEAN API HANDLERS
func handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	path := strings.TrimPrefix(r.URL.Path, "/stats")

	if path == "" || path == "/" {
		// Pool stats
		stats, err := GetPoolStats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(stats)
		return
	}

	// Remove leading slash for miner address
	minerAddr := strings.TrimPrefix(path, "/")
	if minerAddr == "" {
		http.Error(w, "Missing miner address", http.StatusBadRequest)
		return
	}

	// Miner stats
	stats, err := GetMinerStats(minerAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(stats)
}

// ✅ START CLEAN API SERVER
func startAPI() {
	addr := ":" + strconv.Itoa(int(config.Cfg.MasterConfig.ApiPort))

	http.HandleFunc("/stats", handleStats)
	http.HandleFunc("/stats/", handleStats)

	logger.Info("🌐 Pure SQLite API server starting on", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatal("API server failed:", err)
	}
}

// Bridge function for compatibility
func StartApiServer() {
	go startAPI()
}


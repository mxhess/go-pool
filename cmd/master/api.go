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
	"go-pool/config"
	"go-pool/logger"
	"go-pool/pkg/ledger"
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
	if math.IsNaN(val) || math.IsInf(val, 0) {
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
		// If miner not found, return zero balances
		balance = &ledger.MinerBalance{
			MinerAddr:        address,
			BalanceConfirmed: 0,
			BalancePending:   0,
			TotalPaid:        0,
			LastUpdated:      time.Now().Unix(),
		}
	}

	// Get recent shares for hashrate calculation
	windowStart := time.Now().Unix() - 3600 // Last hour
	shares, err := Ledger.GetMinerSharesInWindow(address, windowStart)
	if err != nil {
		logger.Warn("Failed to get miner shares:", err)
		shares = []ledger.Share{}
	}

	// Calculate hashrates
	now := time.Now().Unix()
	var hashrate5m, hashrate15m, hashrate1h float64
	var totalDiff5m, totalDiff15m, totalDiff1h uint64

	for _, share := range shares {
		age := now - share.Timestamp
		if age <= 300 { // 5 minutes
			totalDiff5m += share.Difficulty
		}
		if age <= 900 { // 15 minutes
			totalDiff15m += share.Difficulty
		}
		if age <= 3600 { // 1 hour
			totalDiff1h += share.Difficulty
		}
	}

	// Convert to hashrate (H/s)
	if totalDiff5m > 0 {
		hashrate5m = float64(totalDiff5m) / 300.0
	}
	if totalDiff15m > 0 {
		hashrate15m = float64(totalDiff15m) / 900.0
	}
	if totalDiff1h > 0 {
		hashrate1h = float64(totalDiff1h) / 3600.0
	}

	// Get worker count
	workers, err := Ledger.GetActiveWorkers(address, now-3600)
	workerCount := 0
	if err == nil {
		workerCount = len(workers)
	}

	// Get recent withdrawals
	withdrawals, err := Ledger.GetMinerWithdrawals(address, 10)
	if err != nil {
		logger.Warn("Failed to get withdrawals:", err)
		withdrawals = []ledger.Withdrawal{}
	}

	// Format withdrawals for API
	withdrawalList := make([]map[string]interface{}, len(withdrawals))
	for i, w := range withdrawals {
		withdrawalList[i] = map[string]interface{}{
			"txid":   w.TxID,
			"amount": float64(w.Amount) / Coin,
			"time":   w.Timestamp,
		}
	}

	stats := map[string]interface{}{
		"address":         address,
		"balance":         NotNan(Round6(float64(balance.BalanceConfirmed) / Coin)),
		"balance_pending": NotNan(Round6(float64(balance.BalancePending) / Coin)),
		"paid":           NotNan(Round6(float64(balance.TotalPaid) / Coin)),
		"hashrate_5m":    Round0(hashrate5m),
		"hashrate_15m":   Round0(hashrate15m),
		"hashrate_1h":    Round0(hashrate1h),
		"worker_count":   workerCount,
		"last_seen":      balance.LastUpdated,
		"withdrawals":    withdrawalList,
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
		logger.Warn("Failed to get recent blocks:", err)
		blocks = []ledger.BlockInfo{}
	}

	// Get top miners
	topMiners, err := Ledger.GetTopMinersForAPI(10)
	if err != nil {
		logger.Warn("Failed to get top miners:", err)
		topMiners = []ledger.TopMinerAPI{}
	}

	// Calculate pool hashrate from recent shares
	windowStart := time.Now().Unix() - 300 // Last 5 minutes
	shares, err := Ledger.GetSharesInWindow(windowStart)
	if err != nil {
		logger.Warn("Failed to get shares for pool hashrate:", err)
		shares = []ledger.Share{}
	}

	var totalDiff uint64
	for _, share := range shares {
		totalDiff += share.Difficulty
	}
	poolHashrate := float64(totalDiff) / 300.0 // H/s

	// Get active miners/workers count
	activeSince := time.Now().Unix() - 600 // Active in last 10 minutes
	minerCount, err := Ledger.GetActiveMinerCount(activeSince)
	if err != nil {
		logger.Warn("Failed to get active miner count:", err)
		minerCount = 0
	}

	workerCount, err := Ledger.GetActiveWorkerCount(activeSince)
	if err != nil {
		logger.Warn("Failed to get active worker count:", err)
		workerCount = 0
	}

	// Format blocks for API
	blockList := make([]map[string]interface{}, len(blocks))
	for i, b := range blocks {
		blockList[i] = map[string]interface{}{
			"height":    b.Height,
			"hash":      b.Hash,
			"reward":    float64(b.RewardTotal) / Coin,
			"timestamp": b.Timestamp,
			"status":    b.Status,
		}
	}

	// Format top miners for API
	topMinerList := make([]map[string]interface{}, len(topMiners))
	for i, m := range topMiners {
		topMinerList[i] = map[string]interface{}{
			"address":          m.MinerAddr,
			"shares_total":     m.ShareCount,      // Total shares ever submitted
			"shares_in_window": m.SharesInWindow,  // Shares in current PPLNS window
			"workers":          m.WorkerCount,
			"percentage":       Round3(m.Percentage),
			"last_share":       m.LastShareTime,
		}
	}

	stats := map[string]interface{}{
		"pool_hashrate":     Round0(poolHashrate),
		"connected_miners":  minerCount,
		"connected_workers": workerCount,
		"blocks_found":      len(blocks),
		"recent_blocks":     blockList,
		"top_miners":        topMinerList,
		"pool_fee_percent":  config.Cfg.MasterConfig.FeePercent,
		"min_payout":        config.Cfg.MasterConfig.MinWithdrawal,
		"last_update":       time.Now().Unix(),
	}

	logger.Debug("✅ Pool stats retrieved from SQLite ledger")
	return stats, nil
}

// Bridge function for compatibility with old API format
func handleStatsOld(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "max-age=10")

	path := strings.TrimPrefix(r.URL.Path, "/stats")
	
	if path == "" || path == "/" {
		// Pool stats - return old format
		MasterInfo.RLock()
		height := MasterInfo.Height
		blockReward := MasterInfo.BlockReward
		MasterInfo.RUnlock()

		Stats.RLock()
		defer Stats.RUnlock()

		// Get actual blocks from database
		blocks, err := Ledger.GetRecentBlocks(10)
		if err != nil {
			logger.Warn("Failed to get blocks from database:", err)
		}
		
		// Format blocks for API
		var recentBlocks []map[string]interface{}
		if len(blocks) > 0 {
			recentBlocks = make([]map[string]interface{}, len(blocks))
			for i, block := range blocks {
				recentBlocks[i] = map[string]interface{}{
					"height":    block.Height,
					"timestamp": block.Timestamp,
					"reward":    float64(block.RewardTotal) / Coin,
					"hash":      block.Hash,
				}
			}
		}

		// Return old API format
		response := map[string]interface{}{
			"pool_hr":             Stats.PoolHashrate,
			"net_hr":              Stats.NetHashrate,
			"connected_addresses": len(Stats.KnownAddresses),
			"connected_workers":   Stats.Workers,
			"chart": map[string]interface{}{
				"hashrate":  Stats.PoolHashrateChart,
				"workers":   Stats.WorkersChart,
				"addresses": Stats.AddressesChart,
			},
			"num_blocks_found":    len(blocks),
			"recent_blocks_found": recentBlocks,
			"height":              height,
			"last_block":          Stats.LastBlock,
			"reward":              Round3(float64(blockReward) / Coin),
			"pplns_window_seconds": GetPplnsWindow(),
			"withdrawals":          Stats.RecentWithdrawals,
			"pool_fee_percent":    config.Cfg.MasterConfig.FeePercent,
			"payment_threshold":   config.Cfg.MasterConfig.MinWithdrawal,
		}
		
		json.NewEncoder(w).Encode(response)
		return
	}

	// Miner stats - get address
	minerAddr := strings.TrimPrefix(path, "/")
	if minerAddr == "" {
		http.Error(w, "Missing miner address", http.StatusBadRequest)
		return
	}

	// Get from old stats system
	Stats.RLock()
	defer Stats.RUnlock()

	// Get address info from old system
	addrInfo, err := GetAddressInfoCompat(minerAddr)
	if err != nil {
		// Return empty stats if not found
		addrInfo = &AddressInfoCompat{
			Balance:        0,
			BalancePending: 0,
			Paid:           0,
		}
	}

	// Build response in old format
	uw := []UserWithdrawal{}
	for _, v := range Stats.RecentWithdrawals {
		for _, v2 := range v.Destinations {
			if v2.Address == minerAddr {
				uw = append(uw, UserWithdrawal{
					Amount: float64(v2.Amount) / Coin,
					Txid:   v.Txid,
				})
			}
		}
	}

	// Get worker count
	workerCount := 0
	if addressWorkers, exists := Stats.KnownWorkers[minerAddr]; exists {
		for _, lastActive := range addressWorkers {
			if GetCurrentTime()-lastActive <= 3600 {
				workerCount++
			}
		}
	}

	response := map[string]interface{}{
		"hashrate_5m":     NotNan(Round0(Get5mHashrate(minerAddr))),
		"hashrate_10m":    NotNan(Round0(Get15mHashrate(minerAddr))),
		"hashrate_15m":    NotNan(Round0(Get15mHashrate(minerAddr))),
		"balance":         NotNan(Round6(float64(addrInfo.Balance) / Coin)),
		"balance_pending": NotNan(Round6(float64(addrInfo.BalancePending) / Coin)),
		"paid":            NotNan(Round6(float64(addrInfo.Paid) / Coin)),
		"est_pending":     NotNan(Round6(GetEstPendingBalance(minerAddr))),
		"hr_chart":        Stats.HashrateCharts[minerAddr],
		"withdrawals":     uw,
		"worker_count":    workerCount,
	}

	json.NewEncoder(w).Encode(response)
}

// Compatibility types
type UserWithdrawal struct {
	Amount float64 `json:"amount"`
	Txid   string  `json:"txid"`
}

type AddressInfoCompat struct {
	Balance        uint64
	BalancePending uint64
	Paid           uint64
}

// Get address info from somewhere (you might need to implement this based on your old system)
func GetAddressInfoCompat(addr string) (*AddressInfoCompat, error) {
	// TODO: Get this from your existing stats or database
	// For now, return empty
	return &AddressInfoCompat{}, nil
}

// Get current time helper
func GetCurrentTime() uint64 {
	return uint64(time.Now().Unix())
}

// ✅ START CLEAN API SERVER
func startAPI() {
	addr := ":" + strconv.Itoa(int(config.Cfg.MasterConfig.ApiPort))

	// Use old API format for compatibility
	http.HandleFunc("/stats", handleStatsOld)
	http.HandleFunc("/stats/", handleStatsOld)
	
	// Worker stats endpoint
	http.HandleFunc("/workers/", handleWorkers)

	logger.Info("🌐 Pure SQLite API server starting on", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatal("API server failed:", err)
	}
}

// Bridge function for compatibility
func StartApiServer() {
	go startAPI()
}

// Handle worker stats endpoint
func handleWorkers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "max-age=5")

	// Extract miner address from path
	path := strings.TrimPrefix(r.URL.Path, "/workers/")
	minerAddr := strings.TrimSuffix(path, "/")
	
	if minerAddr == "" {
		http.Error(w, "Missing miner address", http.StatusBadRequest)
		return
	}

	// Get PPLNS window start
	windowStart := time.Now().Unix() - int64(GetPplnsWindow())
	
	// Get worker stats from SQLite
	workers, err := Ledger.GetMinerWorkerStats(minerAddr, windowStart)
	if err != nil {
		logger.Error("Failed to get worker stats:", err)
		workers = []ledger.WorkerStats{}
	}

	// Format response
	currentTime := time.Now().Unix()
	response := make([]map[string]interface{}, len(workers))
	for i, worker := range workers {
		// Determine if worker is active (submitted share in last 5 minutes)
		isActive := (currentTime - worker.LastShareTime) < 300
		status := "offline"
		if isActive {
			status = "active"
		} else if (currentTime - worker.LastShareTime) < 600 {
			status = "idle"
		}
		
		response[i] = map[string]interface{}{
			"worker_id":        worker.WorkerID,
			"shares_total":     worker.ShareCount,
			"shares_in_window": worker.SharesInWindow,
			"percentage":       Round3(worker.Percentage),
			"last_share":       worker.LastShareTime,
			"hashrate":         Round0(worker.Hashrate),
			"status":           status,
			"active":           isActive,
		}
	}

	json.NewEncoder(w).Encode(response)
}


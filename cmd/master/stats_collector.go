


package main

import (
    "go-pool/logger"
    "go-pool/pkg/ledger"
    "go-pool/config"
    "time"
)

// StatsCollector replaces the old Stats system with SQLite-based collection
type StatsCollector struct {
    stopChan chan bool
    interval time.Duration
}

// NewStatsCollector creates a new stats collector
func NewStatsCollector(intervalMinutes int) *StatsCollector {
    return &StatsCollector{
        stopChan: make(chan bool),
        interval: time.Duration(intervalMinutes) * time.Minute,
    }
}

// Start begins the stats collection loop
func (sc *StatsCollector) Start() {
    go sc.collectLoop()
}

// Stop halts the stats collection
func (sc *StatsCollector) Stop() {
    close(sc.stopChan)
}

func (sc *StatsCollector) collectLoop() {
    // Collect immediately on start
    sc.collectStats()
    
    ticker := time.NewTicker(sc.interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            sc.collectStats()
        case <-sc.stopChan:
            return
        }
    }
}

func (sc *StatsCollector) collectStats() {
    logger.Debug("Collecting stats for charts...")
    
    now := time.Now().Unix()
    
    // Calculate pool hashrate from recent shares (5 min window)
    windowStart := now - 300
    shares, err := Ledger.GetSharesInWindow(windowStart)
    if err != nil {
        logger.Error("Failed to get shares for pool hashrate:", err)
        return
    }
    
    var totalDiff uint64
    for _, share := range shares {
        totalDiff += share.Difficulty
    }
    poolHashrate := float64(totalDiff) / 300.0
    
    // Get active miners/workers count
    activeSince := now - 600 // Active in last 10 minutes
    minerCount, err := Ledger.GetActiveMinerCount(activeSince)
    if err != nil {
        logger.Error("Failed to get active miner count:", err)
        minerCount = 0
    }
    
    workerCount, err := Ledger.GetActiveWorkerCount(activeSince)
    if err != nil {
        logger.Error("Failed to get active worker count:", err)
        workerCount = 0
    }
    
    // Get network info
    _, _, difficulty, err := Ledger.GetBlockchainInfo()
    if err != nil {
        logger.Error("Failed to get blockchain info:", err)
        difficulty = 1
    }
 
    netHashrate := float64(difficulty) / float64(config.BlockTime)
    
    // Get blocks found in different time windows
    blocks1h, err := sc.getBlocksFoundSince(now - 3600)
    if err != nil {
        logger.Error("Failed to get blocks found 1h:", err)
    }
    
    blocks24h, err := sc.getBlocksFoundSince(now - 86400)
    if err != nil {
        logger.Error("Failed to get blocks found 24h:", err)
    }
    
    // Cap uint64 values to max int64 to prevent SQLite errors
    const maxInt64 = 9223372036854775807 // max value for int64
    
    netHashrateUint := uint64(netHashrate)
    if netHashrateUint > maxInt64 {
        netHashrateUint = maxInt64
    }
    
    if difficulty > maxInt64 {
        difficulty = maxInt64
    }
    
    stats := ledger.PoolStats{
        Timestamp:         now,
        TotalHashrate:     uint64(poolHashrate),
        NetworkHashrate:   netHashrateUint,
        NetworkDifficulty: difficulty,
        ConnectedMiners:   minerCount,
        ConnectedWorkers:  workerCount,
        BlocksFound1h:     len(blocks1h),
        BlocksFound24h:    len(blocks24h),
    }
 
    if err := Ledger.StorePoolStats(stats); err != nil {
        logger.Error("Failed to store pool stats:", err)
    }
    
    // Collect and store miner stats
    sc.collectMinerStats(now)
    
    // Clean up old data (keep 7 days)
    if err := Ledger.CleanupOldChartData(7); err != nil {
        logger.Error("Failed to cleanup old chart data:", err)
    }
    
    logger.Debug("Stats collection completed")
}

func (sc *StatsCollector) collectMinerStats(now int64) {
    // Get all active miners
    activeSince := now - 3600 // Active in last hour
    
    // Get unique miners from recent shares
    shares, err := Ledger.GetSharesInWindow(activeSince)
    if err != nil {
        logger.Error("Failed to get shares for miner stats:", err)
        return
    }
    
    // Group by miner
    minerShares := make(map[string][]ledger.Share)
    for _, share := range shares {
        minerShares[share.MinerAddr] = append(minerShares[share.MinerAddr], share)
    }
    
    // Calculate stats for each miner
    for minerAddr, shares := range minerShares {
        var totalDiff5m, totalDiff15m, totalDiff1h uint64
        now5m := now - 300
        now15m := now - 900
        now1h := now - 3600
        
        workerMap := make(map[string]bool)
        
        for _, share := range shares {
            // Count unique workers
            workerMap[share.WorkerID] = true
            
            // Calculate hashrates for different windows
            if share.Timestamp >= now5m {
                totalDiff5m += share.Difficulty
            }
            if share.Timestamp >= now15m {
                totalDiff15m += share.Difficulty
            }
            if share.Timestamp >= now1h {
                totalDiff1h += share.Difficulty
            }
        }
        
        hashrate5m := float64(totalDiff5m) / 300.0
        hashrate15m := float64(totalDiff15m) / 900.0
        hashrate1h := float64(totalDiff1h) / 3600.0
        
        if err := Ledger.StoreMinerHashrate(minerAddr, hashrate5m, hashrate15m, hashrate1h, len(workerMap)); err != nil {
            logger.Error("Failed to store miner hashrate:", err)
        }
    }
}

// Helper method to get blocks found since a timestamp
func (sc *StatsCollector) getBlocksFoundSince(since int64) ([]ledger.BlockInfo, error) {
    // Get all recent blocks and filter by timestamp
    blocks, err := Ledger.GetRecentBlocks(1000)
    if err != nil {
        return nil, err
    }
    
    var result []ledger.BlockInfo
    for _, block := range blocks {
        if block.Timestamp >= since {
            result = append(result, block)
        }
    }
    
    return result, nil
}



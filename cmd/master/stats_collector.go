



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
    MasterInfo.RLock()
    difficulty := MasterInfo.Difficulty
    MasterInfo.RUnlock()
    
    netHashrate := float64(difficulty) / float64(config.BlockTime)
    
    // Get blocks found in different time windows
    blocks1h, _ := Ledger.GetBlocksFoundSince(now - 3600)
    blocks24h, _ := Ledger.GetBlocksFoundSince(now - 86400)
    
    // Store pool stats using the models.go version
    stats := ledger.PoolStats{
        Timestamp:         now,
        TotalHashrate:     uint64(poolHashrate),
        NetworkHashrate:   uint64(netHashrate),
        NetworkDifficulty: difficulty,
        ConnectedMiners:   minerCount,
        ConnectedWorkers:  workerCount,
        BlocksFound1h:     len(blocks1h),
        BlocksFound24h:    len(blocks24h),
        // Other fields can be calculated as needed
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
    MasterInfo.RLock()
    difficulty := MasterInfo.Difficulty
    MasterInfo.RUnlock()
    
    netHashrate := float64(difficulty) / float64(config.BlockTime)
    
    // Get round shares
    roundShares, err := Ledger.GetRoundShares()
    if err != nil {
        logger.Error("Failed to get round shares:", err)
        roundShares = 0
    }
    
    // Get total blocks found
    blocks, err := Ledger.GetRecentBlocks(9999) // Get all blocks
    if err != nil {
        logger.Error("Failed to get blocks count:", err)
        blocks = []ledger.BlockInfo{}
    }
    
    // Store pool stats
    stats := ledger.PoolStats{
        Timestamp:         now,
        PoolHashrate:      poolHashrate,
        NetworkHashrate:   netHashrate,
        NetworkDifficulty: difficulty,
        ConnectedMiners:   minerCount,
        ConnectedWorkers:  workerCount,
        RoundShares:       roundShares,
        BlocksFound:       len(blocks),
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
    
    // This is a simplified version - you might need to implement GetActiveMiners
    // For now, we'll skip individual miner stats collection
    // You can implement this based on your needs
}



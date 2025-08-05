


package main

import (
    "go-pool/config"
    "go-pool/logger"
    "go-pool/pkg/ledger"
    "math"
    "time"
)

// PPLNSManager handles dynamic PPLNS calculations
type PPLNSManager struct {
    currentN         uint64    // Current N value (number of shares)
    lastBlockHeight  uint64    // Last block we found
    poolLuckHistory  []float64 // Recent pool luck values
}

var pplnsManager *PPLNSManager

// InitPPLNS initializes the PPLNS manager
func InitPPLNS() {
    pplnsManager = &PPLNSManager{
        currentN: config.Cfg.MasterConfig.PPLNSTargetShares,
        poolLuckHistory: make([]float64, 0, 10),
    }
    
    // Try to restore state from last known good values
    // You might want to store currentN in the database
    logger.Info("PPLNS initialized with N =", pplnsManager.currentN)
}

// GetPPLNSWindow returns the current PPLNS window in terms of share difficulty
func (p *PPLNSManager) GetPPLNSWindow() uint64 {
    MasterInfo.RLock()
    netDifficulty := MasterInfo.Difficulty
    MasterInfo.RUnlock()
    
    // Calculate bounds based on network difficulty
    minWindow := uint64(float64(netDifficulty) * config.Cfg.MasterConfig.PPLNSMinWindow)
    maxWindow := uint64(float64(netDifficulty) * config.Cfg.MasterConfig.PPLNSMaxWindow)
    
    // Ensure N is within bounds
    if p.currentN < minWindow {
        p.currentN = minWindow
    } else if p.currentN > maxWindow {
        p.currentN = maxWindow
    }
    
    return p.currentN
}

// OnBlockFoundPPLNS adjusts the PPLNS window based on pool luck
func (p *PPLNSManager) OnBlockFoundPPLNS(height uint64) {
    // Calculate pool luck (actual shares vs expected shares)
    roundShares, err := Ledger.GetRoundShares()
    if err != nil {
        logger.Error("Failed to get round shares:", err)
        return
    }
    
    MasterInfo.RLock()
    expectedShares := MasterInfo.Difficulty
    MasterInfo.RUnlock()
    
    if expectedShares == 0 {
        return
    }
    
    // Luck = expected / actual (1.0 = normal, >1.0 = lucky, <1.0 = unlucky)
    luck := float64(expectedShares) / float64(roundShares)
    
    // Add to history
    p.poolLuckHistory = append(p.poolLuckHistory, luck)
    if len(p.poolLuckHistory) > 10 {
        p.poolLuckHistory = p.poolLuckHistory[1:]
    }
    
    // Calculate average luck
    avgLuck := float64(0)
    for _, l := range p.poolLuckHistory {
        avgLuck += l
    }
    avgLuck /= float64(len(p.poolLuckHistory))
    
    // Adjust N based on luck
    // If unlucky (avgLuck < 1.0), increase N to protect miners
    // If lucky (avgLuck > 1.0), decrease N slightly
    adjustment := 1.0 + (1.0 - avgLuck) * config.Cfg.MasterConfig.PPLNSLuckFactor
    
    p.currentN = uint64(float64(p.currentN) * adjustment)
    p.lastBlockHeight = height
    
    logger.Info("PPLNS adjusted - Luck:", luck, "Avg Luck:", avgLuck, "New N:", p.currentN)
}

// GetSharesForPPLNS returns shares that count for PPLNS distribution
func (p *PPLNSManager) GetSharesForPPLNS() ([]ledger.Share, error) {
    window := p.GetPPLNSWindow()
    
    // Get shares until we reach the window size
    // This should be optimized with a proper SQL query
    allShares, err := Ledger.GetSharesInWindow(0) // Get all recent shares
    if err != nil {
        return nil, err
    }
    
    // Sort by timestamp descending (newest first)
    // Count backwards until we reach our window
    var totalDiff uint64
    var pplnsShares []ledger.Share
    
    for i := len(allShares) - 1; i >= 0; i-- {
        share := allShares[i]
        pplnsShares = append([]ledger.Share{share}, pplnsShares...)
        totalDiff += share.Difficulty
        
        if totalDiff >= window {
            break
        }
    }
    
    return pplnsShares, nil
}

// GetPplnsWindow returns the PPLNS window in seconds (for API compatibility)
// This replaces the old broken implementation
func GetPplnsWindow() int {
    if pplnsManager == nil {
        InitPPLNS()
    }
    
    // Estimate based on current pool hashrate
    window := pplnsManager.GetPPLNSWindow()
    
    // Get pool hashrate
    windowStart := time.Now().Unix() - 300
    shares, _ := Ledger.GetSharesInWindow(windowStart)
    var totalDiff uint64
    for _, share := range shares {
        totalDiff += share.Difficulty
    }
    poolHashrate := float64(totalDiff) / 300.0
    
    if poolHashrate == 0 {
        return 21600 // Default 6 hours
    }
    
    // Estimate seconds = window_diff / hashrate
    seconds := int(float64(window) / poolHashrate)
    
    // Bounds check
    if seconds < 3600 { // Min 1 hour
        seconds = 3600
    } else if seconds > 86400 { // Max 24 hours
        seconds = 86400
    }
    
    return seconds
}


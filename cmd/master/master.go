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
	"go-pool/address"
	"go-pool/config"
	"go-pool/pkg/ledger"
	"go-pool/logger"
	"fmt"
	"net"
	"time"
	
	"github.com/mxhess/go-salvium/rpc"
	"github.com/mxhess/go-salvium/rpc/daemon"
	"github.com/mxhess/go-salvium/rpc/wallet"
)

var statsCollector *StatsCollector


// ✅ PURE SQLITE LEDGER - NO MORE BOLTDB GARBAGE!
var Ledger *ledger.LedgerDB

// RPC Clients
var DaemonRpc *daemon.Client
var WalletRpc *wallet.Client

func main() {
	if !address.IsAddressValid(config.Cfg.PoolAddress) || !address.IsAddressValid(config.Cfg.FeeAddress) {
		logger.Fatal("Pool or fee address are not valid")
	}

	// ✅ INITIALIZE PURE SQLITE LEDGER
	var err error
	Ledger, err = ledger.NewLedgerDB("pool.db")
	if err != nil {
		logger.Fatal("Failed to initialize SQLite ledger:", err)
	}
	defer Ledger.Close()

	logger.Info("🚀 PURE SQLITE MINING POOL STARTED - BOLTDB ELIMINATED!")

	// Initialize PPLNS
	InitPPLNS()

	// Start the stats collector
	statsCollector = NewStatsCollector(5) // Collect every 5 minutes
	statsCollector.Start()
	defer statsCollector.Stop()

	// Initialize RPC clients
	rpcClient, err := rpc.NewClient(config.Cfg.DaemonRpc)
	if err != nil {
		logger.Fatal("Failed to create daemon RPC client:", err)
	}
	DaemonRpc = daemon.NewClient(rpcClient)
	logger.Info("Using daemon RPC " + config.Cfg.DaemonRpc)

	walletClient, err := rpc.NewClient(config.Cfg.MasterConfig.WalletRpc)
	if err != nil {
		logger.Fatal("Failed to create wallet RPC client:", err)
	}
	WalletRpc = wallet.NewClient(walletClient)
	logger.Info("Using wallet RPC " + config.Cfg.MasterConfig.WalletRpc)

	go Updater()
	go StartApiServer()

	// Start stratum server in main thread since it has the accept loop
	startStratum()

}

func startStratum() {
	srv, err := net.Listen("tcp", config.Cfg.MasterConfig.ListenAddress)
	if err != nil {
		logger.Fatal("Failed to start stratum server:", err)
	}
	logger.Info("Master server listening on", config.Cfg.MasterConfig.ListenAddress)

	// Accept slave connections
	for {
		conn, err := srv.Accept()
		if err != nil {
			logger.Error("Failed to accept connection:", err)
			continue
		}
		go HandleSlave(conn)
	}
}

// ✅ PURE SQLITE SHARE SUBMISSION WITH WORKER ID
func OnShareFound(wallet string, diff uint64, numShares uint32, workerID string) {
    logger.Info("Share found - wallet:", wallet, "worker:", workerID, "diff:", diff, "numShares:", numShares)
    if !address.IsAddressValid(wallet) {
        logger.Warn("Invalid wallet address, using fee address")
        wallet = config.Cfg.FeeAddress
    }
    
    // Submit shares to SQLite ledger
    for i := uint32(0); i < numShares; i++ {
        share := ledger.Share{
            MinerAddr:   wallet,
            WorkerID:    workerID,
            Difficulty:  diff,
            Timestamp:   time.Now().Unix(),
        }
        
        err := Ledger.AddShare(share)
        if err != nil {
            logger.Error("Failed to add share:", err)
        }
    }
}

// ✅ PURE SQLITE SHARE SUBMISSION - NO BOLT GARBAGE!
func SubmitShare(nonce uint64, addr string, worker string, diff uint64) {
    logger.Debug("📋 Submitting share via pure SQLite ledger")
    
    // Submit to ledger
    err := Ledger.AddShare(ledger.Share{
        MinerAddr:  addr,
        WorkerID:   worker,
        Difficulty: diff,
        Timestamp:  time.Now().Unix(),
    })
    
    if err != nil {
        logger.Error("Failed to insert share:", err)
    }
    
    // No more Stats.RoundShares += diff
}

// ✅ PURE SQLITE BLOCK FOUND HANDLER with enhanced pplns
// Update BlockFound to use PPLNS distribution:
func BlockFound(nonce uint64, addr string, hash string, height uint64, reward uint64) {
    logger.Info("🎉 BLOCK FOUND! Height:", height, "Hash:", hash)
    
    block := ledger.BlockFound{
        Height:       height,
        Hash:         hash,
        RewardTotal:  reward,
        Timestamp:    time.Now().Unix(),
        Status:       "pending",
    }
    
    // Store block
    if err := Ledger.AddBlockFound(&block); err != nil {
        logger.Error("Failed to insert found block:", err)
        return
    }
    
    // Use PPLNS distribution instead of the old method
    go func() {
        // Wait a bit for confirmations if needed
        time.Sleep(30 * time.Second)
        
        if err := DistributeBlockRewardPPLNS(height, reward); err != nil {
            logger.Error("Failed to distribute block reward:", err)
        }
    }()
}

// DistributeBlockRewardPPLNS distributes rewards using PPLNS
func DistributeBlockRewardPPLNS(height uint64, reward uint64) error {
    logger.Info("Distributing block reward using PPLNS - Height:", height, "Reward:", reward)
    
    // Get PPLNS shares
    shares, err := pplnsManager.GetSharesForPPLNS()
    if err != nil {
        return fmt.Errorf("failed to get PPLNS shares: %w", err)
    }
    
    if len(shares) == 0 {
        logger.Warn("No shares found for PPLNS distribution")
        return nil
    }
    
    // Calculate total difficulty in window
    var totalDiff uint64
    minerDiffs := make(map[string]uint64)
    
    for _, share := range shares {
        totalDiff += share.Difficulty
        minerDiffs[share.MinerAddr] += share.Difficulty
    }
    
    if totalDiff == 0 {
        return fmt.Errorf("total difficulty is zero")
    }
    
    // Calculate fee
    feePercent := config.Cfg.MasterConfig.FeePercent / 100.0
    feeAmount := uint64(float64(reward) * feePercent)
    netReward := reward - feeAmount
    
    // Distribute to miners
    for minerAddr, minerDiff := range minerDiffs {
        percentage := float64(minerDiff) / float64(totalDiff)
        minerReward := uint64(float64(netReward) * percentage)
        
        if minerReward == 0 {
            continue
        }
        
        // Create distribution record
        dist := ledger.Distribution{
            BlockHeight:   height,
            MinerAddr:     minerAddr,
            SharesContrib: minerDiff,
            Percentage:    percentage * 100,
            RewardGross:   minerReward,
            RewardNet:     minerReward,
            CalculatedAt:  time.Now().Unix(),
        }
        
        if err := Ledger.CreateDistribution(dist); err != nil {
            logger.Error("Failed to create distribution for", minerAddr, ":", err)
            continue
        }
        
        // Update miner balance
        balance, err := Ledger.GetBalance(minerAddr)
        if err != nil {
            logger.Error("Failed to get balance for", minerAddr, ":", err)
            continue
        }
        
        // Add to pending balance
        newPending := balance.BalancePending + minerReward
        
        // Update balance in database
        _, err = Ledger.Conn().Exec(`
            INSERT OR REPLACE INTO balances 
            (miner_address, balance_pending, balance_confirmed, total_paid, last_updated)
            VALUES (?, ?, ?, ?, ?)`,
            minerAddr, newPending, balance.BalanceConfirmed, 
            balance.TotalPaid, time.Now().Unix())
        
        if err != nil {
            logger.Error("Failed to update balance for", minerAddr, ":", err)
        }
        
        logger.Info("Distributed", minerReward, "to", minerAddr, 
            "(", percentage*100, "% of shares)")
    }
    
    // Record fee for pool
    if feeAmount > 0 {
        _, err = Ledger.Conn().Exec(`
            INSERT INTO pool_fees (block_height, fee_amount, collected_at)
            VALUES (?, ?, ?)`,
            height, feeAmount, time.Now().Unix())
        
        if err != nil {
            logger.Error("Failed to record pool fee:", err)
        }
    }
    
    // Mark block as processed
    if err := Ledger.MarkBlockProcessed(height); err != nil {
        logger.Error("Failed to mark block as processed:", err)
    }
    
    // Adjust PPLNS window based on luck
    pplnsManager.OnBlockFoundPPLNS(height)
    
    logger.Info("Block reward distribution completed")
    return nil
}

// Update DistributeBlockReward to use the new PPLNS:
func DistributeBlockReward(height uint64, reward uint64) {
    // Just call the PPLNS version
    if err := DistributeBlockRewardPPLNS(height, reward); err != nil {
        logger.Error("PPLNS distribution failed:", err)
    }
}

// Add GetEstPendingBalance implementation:
func GetEstPendingBalance(addr string) float64 {
    if pplnsManager == nil {
        return 0
    }
    
    // Get miner's percentage of current PPLNS window
    percentage, err := pplnsManager.GetMinerSharePercentage(addr)
    if err != nil {
        logger.Warn("Failed to get miner percentage:", err)
        return 0
    }
    
    // Get current block reward
    _, blockReward, _, err := Ledger.GetBlockchainInfo()
    if err != nil {
        logger.Error("Failed to get blockchain info:", err)
        blockReward = 0
    }
 
    // Calculate after fee
    feePercent := config.Cfg.MasterConfig.FeePercent / 100.0
    netReward := float64(blockReward) * (1 - feePercent)
    
    // Miner's estimated pending
    return percentage * netReward / Coin
}



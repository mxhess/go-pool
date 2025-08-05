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
	"context"
	"database/sql"
	"fmt"
	"go-pool/config"
	"go-pool/pkg/ledger"
	"go-pool/logger"
	"sort"
	"sync"
	"time"

	"github.com/mxhess/go-salvium/rpc/wallet"
)

var minHeight uint64
var minHeightMut sync.RWMutex
var minHeightInitialized bool
var lastProcessedHeight uint64 // Track the highest processed transfer

// Add this init function to be called from main() or Updater():
func initTransferTracking() {
	// Load last processed height from database
	lastHeight, err := Ledger.GetLastProcessedHeight()
	if err != nil {
		logger.Error("Failed to get last processed height:", err)
		// Fallback to zero
		minHeight = 0
	} else {
		minHeight = lastHeight + 1
		logger.Info("📊 Starting transfer processing from height:", minHeight)
	}
	minHeightInitialized = true
}

// ✅ PURE SQLITE UPDATER - NO BOLT GARBAGE!
func Updater() {

	// Initialize transfer tracking
	initTransferTracking()

	// Start withdrawal processor
	go func() {
		for {
			go Withdraw()
			time.Sleep(time.Duration(config.Cfg.MasterConfig.WithdrawInterval) * time.Minute)
		}
	}()

	// Main update loop
	for {
		time.Sleep(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		info, err := DaemonRpc.GetInfo(ctx)
		cancel()

		if err != nil {
			logger.Warn(err)
			continue
		}

		// ✅ PURE SQLITE: No locks needed!
		currentHeight, _, _, err := Ledger.GetBlockchainInfo()
		if err != nil {
			logger.Warn("Failed to get blockchain info:", err)
			currentHeight = 0
		}
		
		heightChanged := info.Height != currentHeight
		if heightChanged {
			logger.Info("📊 New height", currentHeight, "->", info.Height)
			// Update blockchain info in SQLite
			if err := Ledger.UpdateBlockchainInfo(info.Height, 0, info.Difficulty); err != nil {
				logger.Error("Failed to update blockchain info:", err)
			}
			config.BlockTime = info.Target
		}

		if heightChanged {
			// Process transfers with PURE SQLite
			go func() {
				updated := CheckWithdraw()
				if updated {
					logger.Info("CheckWithdraw(): some balances have been updated")
				} else {
					logger.Debug("CheckWithdraw(): no balances have been updated")
				}
			}()
			go UpdatePendingBals()
		}

		go UpdateReward()
	}
}

func UpdateReward() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	info, err := DaemonRpc.GetLastBlockHeader(ctx)
	cancel()
	if err != nil {
		logger.Warn(err)
		return
	}
	
	// Get current blockchain info
	height, _, difficulty, err := Ledger.GetBlockchainInfo()
	if err != nil {
		logger.Error("Failed to get blockchain info:", err)
		return
	}
	
	// Update with new block reward
	if err := Ledger.UpdateBlockchainInfo(height, info.BlockHeader.Reward, difficulty); err != nil {
		logger.Error("Failed to update block reward:", err)
	}
}

// ✅ PURE SQLITE TRANSFER PROCESSOR - INFINITE LOOPS DESTROYED!
func UpdatePendingBals() {
	logger.Debug("🔄 Updating user balances via pure SQLite ledger")

	curAddy := config.Cfg.PoolAddress
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	indices, err := WalletRpc.GetAddressIndex(ctx, curAddy)
	if err != nil {
		logger.Warn(err)
		return
	}

	minHeightMut.RLock()
	currentMinHeight := minHeight
	minHeightMut.RUnlock()

	transfers, err := WalletRpc.GetTransfers(ctx, wallet.GetTransfersParams{
		In:             true,
		AccountIndex:   indices.Index.Major,
		SubaddrIndices: []uint{indices.Index.Minor},
		FilterByHeight: true,
		MinHeight:      currentMinHeight,
	})

	if err != nil {
		logger.Warn(err)
		return
	}

	// Sort transfers by ascending height
	sort.SliceStable(transfers.In, func(i, j int) bool {
		return transfers.In[i].Height < transfers.In[j].Height
	})

	// Count only unprocessed transfers for logging
	unprocessedCount := 0
	for _, vt := range transfers.In {
		processed, err := Ledger.IsTransferProcessed(vt.Txid)
		if err != nil || !processed {
			unprocessedCount++
		}
	}
	
	if unprocessedCount > 0 {
		logger.Dev("🔍 Processing", unprocessedCount, "new transfers via SQLite ledger")
	}

	// Process transfers
	for _, vt := range transfers.In {
		// Check if already processed
		processed, err := Ledger.IsTransferProcessed(vt.Txid)
		if err != nil {
			logger.Error("❌ Error checking transfer:", err)
			continue
		}

		if processed {
			// Update minHeight if this processed transfer is newer
			minHeightMut.Lock()
			if vt.Height >= minHeight {
				minHeight = vt.Height + 1
			}
			minHeightMut.Unlock()
			continue
		}

		// Process new transfer
		logger.Info("🆕 Processing NEW transfer:", vt.Txid, "Height:", vt.Height, "Amount:", float64(vt.Amount)/1e8, "SAL")

		if err := processTransfer(&vt); err != nil {
			logger.Error("❌ Failed to process transfer:", err)
			continue
		}

		// Mark as processed
		err = Ledger.MarkTransferProcessed(vt.Txid, vt.Height, vt.Amount)
		if err != nil {
			logger.Error("❌ Error marking transfer processed:", err)
		} else {
			logger.Info("✅ Transfer", vt.Txid, "processed and marked - WILL NEVER REPROCESS!")

			// Update minHeight for next run
			minHeightMut.Lock()
			if vt.Height >= minHeight {
				minHeight = vt.Height + 1
			}
			minHeightMut.Unlock()
		}
	}

	if len(transfers.In) > 0 {
		// Get the highest height we've seen
		highestHeight := transfers.In[len(transfers.In)-1].Height
       
		// Save it to the database
		if err := Ledger.UpdateLastProcessedHeight(highestHeight); err != nil {
			logger.Error("Failed to update last processed height:", err)
		}
       
		// Update in-memory tracking
		minHeightMut.Lock()
		minHeight = highestHeight + 1
		minHeightMut.Unlock()
	}

}

// ✅ PURE SQLITE TRANSFER PROCESSOR
func processTransfer(transfer *wallet.TransferInfo) error {
	// Check if block already processed
	blockProcessed, err := Ledger.IsBlockProcessed(transfer.Height)
	if err != nil {
		return fmt.Errorf("failed to check block: %w", err)
	}

	if blockProcessed {
		logger.Dev("⏭️ Block", transfer.Height, "already processed")
		return nil
	}

	// Create block record
	block := ledger.BlockFound{
		Height:      transfer.Height,
		Hash:        transfer.Txid,
		RewardTotal: transfer.Amount,
		Timestamp:   int64(transfer.Timestamp),
		FoundAt:     time.Now().Unix(),
		Status:      "processing",
	}

	if err := Ledger.CreateBlock(block); err != nil {
		return fmt.Errorf("failed to create block: %w", err)
	}

	// Calculate PPLNS distributions
	distributions, err := calculateDistributions(transfer)
	if err != nil {
		return fmt.Errorf("failed to calculate distributions: %w", err)
	}

	// Use transaction for atomicity
	err = Ledger.WithTransaction(func(tx *sql.Tx) error {
		// Create distributions
		for _, dist := range distributions {
			if err := Ledger.CreateDistribution(dist); err != nil {
				return err
			}
		}

		// Queue payments
		for _, dist := range distributions {
			payment := ledger.Payment{
				BlockHeight:   &transfer.Height,
				RecipientAddr: dist.MinerAddr,
				Amount:        dist.RewardNet,
				Fee:           0,
				Status:        "pending",
				CreatedAt:     time.Now().Unix(),
			}

			if err := Ledger.CreatePayment(payment); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create distributions: %w", err)
	}

	// Mark block as completed
	if err := Ledger.MarkBlockProcessed(transfer.Height); err != nil {
		return fmt.Errorf("failed to mark block processed: %w", err)
	}

	logger.Info("✅ Block", transfer.Height, "fully processed with", len(distributions), "distributions")
	return nil
}

// ✅ PURE SQLITE PPLNS CALCULATION
func calculateDistributions(transfer *wallet.TransferInfo) ([]ledger.Distribution, error) {
	// Get shares in PPLNS window
	windowStart := time.Now().Unix() - int64(21600) // 6 hours default
	shares, err := Ledger.GetSharesInWindow(windowStart)
	if err != nil {
		return nil, err
	}

	if len(shares) == 0 {
		logger.Warn("⚠️ No shares in PPLNS window for block", transfer.Height)
		return nil, nil
	}

	// Calculate miner shares
	minerShares := make(map[string]uint64)
	var totalShares uint64

	for _, share := range shares {
		minerShares[share.MinerAddr] += share.Difficulty
		totalShares += share.Difficulty
	}

	// Calculate distributions
	var distributions []ledger.Distribution
	feePercent := config.Cfg.MasterConfig.FeePercent

	for minerAddr, shareCount := range minerShares {
		percentage := float64(shareCount) / float64(totalShares)
		rewardGross := uint64(float64(transfer.Amount) * percentage)
		rewardNet := uint64(float64(rewardGross) * (100 - feePercent) / 100)

		distribution := ledger.Distribution{
			BlockHeight:   transfer.Height,
			MinerAddr:     minerAddr,
			SharesContrib: shareCount,
			Percentage:    percentage,
			RewardGross:   rewardGross,
			RewardNet:     rewardNet,
			CalculatedAt:  time.Now().Unix(),
		}

		distributions = append(distributions, distribution)
	}

	// Add pool fee
	totalNetReward := uint64(float64(transfer.Amount) * (100 - feePercent) / 100)
	poolFee := transfer.Amount - totalNetReward

	feeDistribution := ledger.Distribution{
		BlockHeight:   transfer.Height,
		MinerAddr:     config.Cfg.FeeAddress,
		SharesContrib: 0,
		Percentage:    feePercent / 100,
		RewardGross:   poolFee,
		RewardNet:     poolFee,
		CalculatedAt:  time.Now().Unix(),
	}

	distributions = append(distributions, feeDistribution)

	logger.Info("💰 Calculated", len(distributions), "distributions for block", transfer.Height)
	return distributions, nil
}


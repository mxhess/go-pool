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

// Stats is defined in stats.go, removing duplicate declaration

// ✅ PURE SQLITE UPDATER - NO BOLT GARBAGE!
func Updater() {
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

		MasterInfo.Lock()
		if info.Height != MasterInfo.Height {
			logger.Info("📊 New height", MasterInfo.Height, "->", info.Height)
			MasterInfo.Height = info.Height
			MasterInfo.Lock()
			MasterInfo.Difficulty = info.Difficulty
			MasterInfo.Unlock()

			config.BlockTime = info.Target
			MasterInfo.Unlock()

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
		} else {
			MasterInfo.Unlock()
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
	MasterInfo.Lock()
	defer MasterInfo.Unlock()
	MasterInfo.BlockReward = info.BlockHeader.Reward

}

var minHeight uint64
var minHeightMut sync.RWMutex
var lastProcessedHeight uint64 // Track the highest processed transfer

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

	minHeightMut.Lock()
	// Update minHeight if we've processed newer transfers
	if lastProcessedHeight > minHeight {
		minHeight = lastProcessedHeight + 1
	}
	
	transfers, err := WalletRpc.GetTransfers(ctx, wallet.GetTransfersParams{
		In:             true,
		AccountIndex:   indices.Index.Major,
		SubaddrIndices: []uint{indices.Index.Minor},
		FilterByHeight: true,
		MinHeight:      minHeight,
	})
	minHeightMut.Unlock()

	if err != nil {
		logger.Warn(err)
		return
	}

	// Sort transfers by ascending height
	sort.SliceStable(transfers.In, func(i, j int) bool {
		return transfers.In[i].Height < transfers.In[j].Height
	})

	if len(transfers.In) > 0 {
		logger.Dev("🔍 Processing", len(transfers.In), "transfers via SQLite ledger")
	}

	// ✅ PURE SQLITE TRANSFER PROCESSING - ANTI-BUG PROTECTION!
	for _, vt := range transfers.In {
		// 🚫 THE BUG KILLER: Check if already processed
		processed, err := Ledger.IsTransferProcessed(vt.Txid)
		if err != nil {
			logger.Error("❌ Error checking transfer:", err)
			continue
		}

		if processed {
			// Don't log at DEV level for known processed transfers
			continue
		}

		// Process new transfer
		logger.Info("🆕 Processing NEW transfer:", vt.Txid, "Height:", vt.Height, "Amount:", float64(vt.Amount)/1e8, "SAL")

		if err := processTransfer(&vt); err != nil {
			logger.Error("❌ Failed to process transfer:", err)
			continue
		}

		// 🛡️ Mark as processed - PREVENTS FUTURE INFINITE LOOPS!
		err = Ledger.MarkTransferProcessed(vt.Txid, vt.Height, vt.Amount)
		if err != nil {
			logger.Error("❌ Error marking transfer processed:", err)
		} else {
			logger.Info("✅ Transfer", vt.Txid, "processed and marked - WILL NEVER REPROCESS!")
			
			// Update lastProcessedHeight
			minHeightMut.Lock()
			if vt.Height > lastProcessedHeight {
				lastProcessedHeight = vt.Height
			}
			minHeightMut.Unlock()
		}
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


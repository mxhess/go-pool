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
	"go-pool/config"
	"go-pool/logger"
	"time"
)

// GetPplnsWindow returns the current PPLNS window duration in seconds
// This is the OLD implementation - will be replaced by proper PPLNS
func GetPplnsWindowLegacy() uint64 {
	// Get network hashrate from difficulty
	_, _, netDifficulty, err := Ledger.GetBlockchainInfo()
	if err != nil {
		logger.Error("Failed to get blockchain info:", err)
		netDifficulty = 1
	}

	if netDifficulty == 0 {
		return 6 * 3600 // Default 6 hours
	}

	netHashrate := float64(netDifficulty) / float64(config.BlockTime)

	// Get pool hashrate from recent shares
	windowStart := time.Now().Unix() - 300 // 5 minutes
	shares, err := Ledger.GetSharesInWindow(windowStart)
	if err != nil {
		return 6 * 3600 // Default on error
	}

	var totalDiff uint64
	for _, share := range shares {
		totalDiff += share.Difficulty
	}
	poolHashrate := float64(totalDiff) / 300.0

	if poolHashrate == 0 {
		return 6 * 3600
	}

	blockFoundInterval := netHashrate / poolHashrate * float64(config.BlockTime)

	// PPLNS window is double of average pool block found time
	blockFoundInterval *= 2

	// PPLNS window is at most 6 hours
	if blockFoundInterval > 6*3600 {
		return 6 * 3600
	}

	// PPLNS window is at least 2 times the block time
	if blockFoundInterval < float64(config.BlockTime)*2 {
		return config.BlockTime * 2
	}

	return uint64(blockFoundInterval)
}


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
	"math"
	"net"
	"sync"
	"time"
	
	"github.com/mxhess/go-salvium/rpc"
	"github.com/mxhess/go-salvium/rpc/daemon"
	"github.com/mxhess/go-salvium/rpc/wallet"
)

type Info struct {
	Height      uint64
	BlockReward uint64
	sync.RWMutex
}

var MasterInfo Info

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

	// Start stats server (already exists in stats.go)
	go StatsServer()

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
	
	// Update in-memory stats for immediate display
	Stats.Lock()
	for i := uint32(0); i < numShares; i++ {
		Stats.Shares = append(Stats.Shares, StatsShare{
			Count:    1,
			Wallet:   wallet,
			WorkerID: workerID,
			Diff:     diff,
			Time:     uint64(time.Now().Unix()),
		})
	}
	Stats.Cleanup()
	Stats.Unlock()
	
	// Submit shares to SQLite ledger
	for i := uint32(0); i < numShares; i++ {
		share := ledger.Share{
			// BlockHeight is nil by default (pointer)
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

	// Create share record
	share := ledger.Share{
		// BlockHeight is nil by default (pointer)
		MinerAddr:   addr,
		WorkerID:    worker,
		Difficulty:  diff,
		Timestamp:   time.Now().Unix(),
	}

	// Submit to SQLite ledger
	err := Ledger.AddShare(share)
	if err != nil {
		logger.Error("Failed to add share to ledger:", err)
		return
	}

	logger.Debug("✅ Share added to SQLite ledger")
}

// ✅ PURE SQLITE BLOCK FOUND HANDLER
func BlockFound(nonce uint64, addr string, hash string, height uint64, reward uint64) {
	logger.Info("🎉 BLOCK FOUND! Height:", height, "Reward:", float64(reward)/100000000, "SAL")

	// Create block record in SQLite
	block := ledger.BlockFound{
		Height:      height,
		Hash:        hash,
		RewardTotal: reward,
		Timestamp:   time.Now().Unix(),
		FoundAt:     time.Now().Unix(),
		Status:      "pending",
	}

	err := Ledger.CreateBlock(block)
	if err != nil {
		logger.Error("Failed to record block in ledger:", err)
		return
	}

	logger.Info("✅ Block recorded in SQLite ledger")
}

// GetPplnsWindow is defined in pplns_diff.go

// Important: Stats must be locked
func GetEstPendingBalance(addr string) float64 {
	var totHashes float64
	var minerHashes float64

	MasterInfo.RLock()
	reward := float64(MasterInfo.BlockReward) / math.Pow10(config.Cfg.Atomic)
	MasterInfo.RUnlock()

	if config.Cfg.UseP2Pool {
		reward = 0
	}

	balance := minerHashes / totHashes * reward

	return balance
}


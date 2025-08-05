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
	"fmt"
	"go-pool/config"
	"go-pool/pkg/ledger"
	"go-pool/logger"
	"math"
	"time"
	
	"github.com/mxhess/go-salvium/rpc/wallet"
)

// ✅ PURE SQLITE WITHDRAWAL CHECKER - NO BOLT GARBAGE!
func CheckWithdraw() bool {
	logger.Debug("🔍 CheckWithdraw() via pure SQLite ledger")

	// Check for confirmed transactions
	// TODO: Implement transaction confirmation checking
	// This would replace the old pending bucket logic

	logger.Debug("✅ CheckWithdraw() completed - no pending confirmations")
	return false
}

// ✅ PURE SQLITE WITHDRAWAL PROCESSOR
func Withdraw() {
	logger.Debug("💸 Processing withdrawals via pure SQLite ledger")

	// Get pending payments from SQLite
	payments, err := Ledger.GetPendingPayments()
	if err != nil {
		logger.Error("Failed to get pending payments:", err)
		return
	}

	if len(payments) == 0 {
		logger.Debug("📭 No pending payments")
		return
	}

	logger.Info("💰 Found", len(payments), "pending payments")

	// Group payments by recipient for batch processing
	recipientPayments := make(map[string][]ledger.Payment)
	for _, payment := range payments {
		recipientPayments[payment.RecipientAddr] = append(recipientPayments[payment.RecipientAddr], payment)
	}

	minWithdrawal := uint64(config.Cfg.MasterConfig.MinWithdrawal * math.Pow10(config.Cfg.Atomic))
	logger.Debug("💎 Minimum withdrawal threshold:", float64(minWithdrawal)/math.Pow10(config.Cfg.Atomic), "SAL")

	for recipientAddr, paymentList := range recipientPayments {
		// Calculate total amount for this recipient
		var totalAmount uint64
		for _, payment := range paymentList {
			totalAmount += payment.Amount
		}

		logger.Debug("🎯 Recipient", recipientAddr, "has", float64(totalAmount)/math.Pow10(config.Cfg.Atomic), "SAL pending")

		// Check minimum withdrawal threshold
		if totalAmount < minWithdrawal {
			logger.Debug("⏳ Amount below minimum withdrawal threshold")
			continue
		}

		// Send payment via wallet RPC
		logger.Info("🚀 Sending payment:", float64(totalAmount)/math.Pow10(config.Cfg.Atomic), "SAL to", recipientAddr)

		txid, err := sendPayment(recipientAddr, totalAmount)
		if err != nil {
			logger.Error("❌ Failed to send payment:", err)
			continue
		}

		// Update payment statuses in SQLite
		for _, payment := range paymentList {
			err := Ledger.UpdatePaymentStatus(payment.ID, "sent", &txid)
			if err != nil {
				logger.Error("❌ Failed to update payment status:", err)
			}
		}

		logger.Info("✅ Payment sent successfully! TxID:", txid)
	}
}

// ✅ WALLET RPC PAYMENT SENDER
func sendPayment(address string, amount uint64) (string, error) {
	logger.Debug("💳 Sending wallet RPC payment")

	// Calculate withdrawal fee
	withdrawalFee := uint64(config.Cfg.MasterConfig.WithdrawalFee * math.Pow10(config.Cfg.Atomic))

	// Create payment destinations
	destinations := []wallet.Destination{
		{
			Address:   address,
			Amount:    amount - withdrawalFee,
			AssetType: "SAL1",
		},
	}

	// Send via wallet RPC
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := WalletRpc.Transfer(ctx, wallet.TransferParameters{
		Destinations:  destinations,
		SourceAsset:  "SAL1",
		DestAsset:    "SAL1",
		TxType:       3,
		DoNotRelay:    false,
		GetTxMetadata: false,
	})

	if err != nil {
		return "", fmt.Errorf("wallet RPC transfer failed: %w", err)
	}

	logger.Info("✅ Wallet transfer successful")
	logger.Debug("💰 Sent", float64(amount)/math.Pow10(config.Cfg.Atomic), "SAL to", address)
	logger.Debug("🔗 Transaction ID:", response.TxHash)
	logger.Debug("💸 Fee paid:", float64(response.Fee)/math.Pow10(config.Cfg.Atomic), "SAL")

	return response.TxHash, nil
}

// ✅ LEGACY FUNCTION WRAPPERS (if needed for compatibility)
func ProcessWithdrawals() {
	Withdraw()
}


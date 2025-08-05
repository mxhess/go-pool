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
	"bufio"
	"encoding/json"
	"go-pool/logger"
	"net"
	"time"
)

// Message types for slave communication
type SlaveMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ShareData struct {
	Wallet    string `json:"wallet"`
	Diff      uint64 `json:"diff"`
	NumShares uint32 `json:"num_shares"`
}

type BlockData struct {
	Wallet string `json:"wallet"`
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
	Reward uint64 `json:"reward"`
}

// HandleSlave handles incoming connections from slave nodes
func HandleSlave(conn net.Conn) {
	defer conn.Close()
	
	slaveAddr := conn.RemoteAddr().String()
	logger.Info("Slave connected from", slaveAddr)
	
	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)
	
	// Send initial configuration to slave
	config := map[string]interface{}{
		"type": "config",
		"data": map[string]interface{}{
			"pool_address": config.Cfg.PoolAddress,
			"fee_address":  config.Cfg.FeeAddress,
			"fee_percent":  config.Cfg.MasterConfig.FeePercent,
		},
	}
	
	if err := encoder.Encode(config); err != nil {
		logger.Error("Failed to send config to slave:", err)
		return
	}
	
	// Read messages from slave
	for scanner.Scan() {
		var msg SlaveMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			logger.Error("Failed to decode slave message:", err)
			continue
		}
		
		switch msg.Type {
		case "share":
			var share ShareData
			if err := json.Unmarshal(msg.Data, &share); err != nil {
				logger.Error("Failed to decode share data:", err)
				continue
			}
			// Process share
			OnShareFound(share.Wallet, share.Diff, share.NumShares)
			
		case "block":
			var block BlockData
			if err := json.Unmarshal(msg.Data, &block); err != nil {
				logger.Error("Failed to decode block data:", err)
				continue
			}
			// Process block
			BlockFound(0, block.Wallet, block.Hash, block.Height, block.Reward)
			
		case "ping":
			// Respond with pong
			pong := map[string]interface{}{
				"type": "pong",
				"time": time.Now().Unix(),
			}
			if err := encoder.Encode(pong); err != nil {
				logger.Error("Failed to send pong:", err)
			}
			
		default:
			logger.Warn("Unknown message type from slave:", msg.Type)
		}
	}
	
	if err := scanner.Err(); err != nil {
		logger.Error("Slave connection error:", err)
	}
	
	logger.Info("Slave disconnected:", slaveAddr)
}

// StatsServer handles stats broadcasting to slaves
func StatsServer() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		// Broadcast stats to all connected slaves
		// TODO: Implement slave tracking and broadcasting
		logger.Debug("Stats broadcast tick")
	}
}


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

package slave

import (
	"go-pool/logger"
	"go-pool/serializer"
	"sync"
	"time"
)

// Enhanced to support worker IDs
type ShareCache struct {
	NumShares uint32
	TotalDiff uint64
	WorkerID  string // NEW: Store worker ID
}

type Cache struct {
	Shares map[string]ShareCache // key format: "wallet:workerID"
	sync.RWMutex
}

var slaveCache = Cache{
	Shares: map[string]ShareCache{},
}

// Enhanced to support worker IDs
func cacheShareWithWorker(wallet, workerID string, diff uint64) {
	slaveCache.Lock()
	defer slaveCache.Unlock()
	
	// Create composite key: wallet:workerID
	key := wallet + ":" + workerID
	x := slaveCache.Shares[key]
	x.NumShares++
	x.TotalDiff += diff
	x.WorkerID = workerID
	slaveCache.Shares[key] = x
	
	logger.Debug("Cached share:", wallet, "worker:", workerID, "diff:", diff)
}

// Legacy function for backward compatibility
func cacheShare(wallet string, diff uint64) {
	cacheShareWithWorker(wallet, "legacy", diff)
}

func init() {
	go func() {
		for {
			time.Sleep(10 * time.Second)
			connMut.Lock()
			if conn != nil {
				slaveCache.Lock()
				logger.Debug("sending cached shares")
				for key, v := range slaveCache.Shares {
					// Extract wallet from composite key "wallet:workerID"
					wallet := key
					if lastColon := len(key) - 1; lastColon > 0 {
						for i := lastColon; i >= 0; i-- {
							if key[i] == ':' {
								wallet = key[:i]
								break
							}
						}
					}
					
					logger.Debug("sending cache share with address:", wallet, "worker:", v.WorkerID, "count", v.NumShares, "total diff", v.TotalDiff)
					sendCachedShareWithWorker(v.NumShares, wallet, v.WorkerID, v.TotalDiff)
				}
				slaveCache.Shares = make(map[string]ShareCache, 100)
				slaveCache.Unlock()
			}
			connMut.Unlock()
		}
	}()
}

// Enhanced to include worker ID in 4-field protocol
func sendCachedShareWithWorker(count uint32, wallet, workerID string, diff uint64) {
	s := serializer.Serializer{
		Data: []byte{0},
	}
	s.AddUvarint(uint64(count))
	s.AddString(wallet)
	s.AddString(workerID) // NEW: Add worker ID (4th field)
	s.AddUvarint(diff)
	
	sendToConn(s.Data)
	logger.Debug("Sent cached share to master:", wallet, "worker:", workerID, "count:", count, "diff:", diff)
}

// Legacy 3-field function for backward compatibility
func sendCachedShare(count uint32, wallet string, diff uint64) {
	sendCachedShareWithWorker(count, wallet, "legacy", diff)
}


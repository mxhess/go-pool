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
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"go-pool/config"
	"go-pool/logger"
	"go-pool/serializer"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

var conn net.Conn
var connMut sync.RWMutex

const Overhead = 40

// NEW: Cached share structure
type CachedShare struct {
	Wallet   string    `json:"wallet"`
	WorkerID string    `json:"worker_id"`
	Diff     uint64    `json:"diff"`
	Time     time.Time `json:"time"`
	ID       string    `json:"id"` // Unique ID for tracking
}

// NEW: Cache management
var shareCache []CachedShare
var cacheMutex sync.RWMutex
const CACHE_FILE = "cached_shares.json"

// NEW: Initialize cache from file on startup
func init() {
	loadShareCache()
}

// NEW: Load cached shares from disk
func loadShareCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	
	data, err := os.ReadFile(CACHE_FILE)
	if err != nil {
		logger.Info("No cached shares file found (this is normal on first run)")
		shareCache = make([]CachedShare, 0)
		return
	}
	
	err = json.Unmarshal(data, &shareCache)
	if err != nil {
		logger.Error("Failed to load cached shares:", err)
		shareCache = make([]CachedShare, 0)
		return
	}
	
	logger.Info("Loaded", len(shareCache), "cached shares from disk")
}

// NEW: Save cached shares to disk
func saveShareCache() {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	
	data, err := json.Marshal(shareCache)
	if err != nil {
		logger.Error("Failed to marshal cached shares:", err)
		return
	}
	
	err = os.WriteFile(CACHE_FILE, data, 0600)
	if err != nil {
		logger.Error("Failed to save cached shares:", err)
		return
	}
	
	logger.Debug("Saved", len(shareCache), "shares to cache file")
}

// NEW: Add share to cache
func cacheShare(wallet, workerID string, diff uint64) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	
	// Generate unique ID for this share
	shareID := hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	
	cached := CachedShare{
		Wallet:   wallet,
		WorkerID: workerID,
		Diff:     diff,
		Time:     time.Now(),
		ID:       shareID,
	}
	
	shareCache = append(shareCache, cached)
	
	logger.Info("Cached share:", wallet, "worker:", workerID, "diff:", diff, "cache_size:", len(shareCache))
	
	// Save to disk immediately
	go saveShareCache()
}

// NEW: Send cached shares when reconnected
func sendCachedShares() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	
	if len(shareCache) == 0 {
		return
	}
	
	logger.Info("Sending", len(shareCache), "cached shares to master")
	
	// Send all cached shares
	for _, cached := range shareCache {
		s := serializer.Serializer{}
		s.AddUint8(0) // Share Found packet type
		s.AddUvarint(1) // number of shares
		s.AddString(cached.Wallet)
		s.AddString(cached.WorkerID)
		s.AddUvarint(cached.Diff)
		
		sendToConnDirect(s.Data)
		logger.Debug("Sent cached share:", cached.Wallet, "worker:", cached.WorkerID, "diff:", cached.Diff)
	}
	
	// Clear cache after sending
	shareCache = make([]CachedShare, 0)
	logger.Info("All cached shares sent and cleared")
	
	// Remove cache file
	os.Remove(CACHE_FILE)
}

// Enhanced share sending with caching
func SendShareWithWorker(wallet, workerID string, diff uint64) {
	connMut.RLock()
	connected := (conn != nil)
	connMut.RUnlock()
	
	if !connected {
		logger.Warn("Master disconnected, caching share:", wallet, "worker:", workerID)
		cacheShare(wallet, workerID, diff)
		return
	}
	
	s := serializer.Serializer{}
	s.AddUint8(0) // Share Found packet type
	s.AddUvarint(1) // number of shares
	s.AddString(wallet)
	s.AddString(workerID)
	s.AddUvarint(diff)
	
	sendToConn(s.Data)
	logger.Debug("Sent share to master:", wallet, "worker:", workerID, "diff:", diff)
}

func StartSlaveClient() {
out:
	for {
		logger.Info("Connecting to master server:", config.Cfg.SlaveConfig.MasterAddress)

		var err error
		conn, err = net.Dial("tcp", config.Cfg.SlaveConfig.MasterAddress)

		if err != nil {
			logger.Error(err)
			time.Sleep(time.Second)
			continue out
		}
		
		logger.Info("Connected to master server")
		
		// NEW: Send any cached shares immediately upon reconnection
		go func() {
			time.Sleep(1 * time.Second) // Give connection a moment to stabilize
			sendCachedShares()
		}()

		for {
			lenBuf := make([]byte, 2+Overhead)
			_, err := io.ReadFull(conn, lenBuf)
			if err != nil {
				logger.Warn("Connection to master lost:", err)
				connMut.Lock()
				conn = nil
				connMut.Unlock()
				time.Sleep(time.Second)
				continue out
			}
			lenBuf, err = Decrypt(lenBuf)
			if err != nil {
				logger.Warn(err)
				connMut.Lock()
				conn = nil
				connMut.Unlock()
				time.Sleep(time.Second)
				continue out
			}
			len := int(lenBuf[0]) | (int(lenBuf[1]) << 8)

			// read the actual message
			buf := make([]byte, len+Overhead)
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				logger.Warn(err)
				connMut.Lock()
				conn = nil
				connMut.Unlock()
				time.Sleep(time.Second)
				continue out
			}
			buf, err = Decrypt(buf)
			if err != nil {
				logger.Warn(err)
				connMut.Lock()
				conn = nil
				connMut.Unlock()
				time.Sleep(time.Second)
				continue out
			}
			logger.Net("Received message:", hex.EncodeToString(buf))
			OnMessage(buf)
		}
	}
}

func OnMessage(b []byte) {
	d := serializer.Deserializer{
		Data: b,
	}

	packet := d.ReadUint8()

	switch packet {
	// No incoming messages from master currently
	}
}

// Legacy function for backward compatibility
func SendShare(wallet string, diff uint64) {
	SendShareWithWorker(wallet, "legacy", diff)
}

func SendBlockFound(height, reward uint64, hash []byte) {
	s := serializer.Serializer{
		Data: []byte{1},
	}

	s.AddUvarint(height)
	s.AddUvarint(reward)
	s.AddFixedByteArray(hash, 32)

	sendToConn(s.Data)
}

func SendStats(nrMiners int) {
	s := serializer.Serializer{
		Data: []byte{2},
	}
	s.AddUvarint(uint64(nrMiners))

	sendToConn(s.Data)
}

func SendShareFound(height uint64) {
	s := serializer.Serializer{
		Data: []byte{3},
	}

	s.AddUvarint(height)

	sendToConn(s.Data)
}

// NEW: Direct send without checking connection (for cached shares)
func sendToConnDirect(data []byte) {
	var dataLenBin = make([]byte, 0, 2)
	dataLenBin = binary.LittleEndian.AppendUint16(dataLenBin, uint16(len(data)))
	conn.Write(Encrypt(dataLenBin))
	conn.Write(Encrypt(data))
}

func sendToConn(data []byte) {
	connMut.RLock()
	defer connMut.RUnlock()
	
	if conn == nil {
		logger.Error("SendToConn: Connection is nil")
		return
	}
	
	var dataLenBin = make([]byte, 0, 2)
	dataLenBin = binary.LittleEndian.AppendUint16(dataLenBin, uint16(len(data)))
	conn.Write(Encrypt(dataLenBin))
	conn.Write(Encrypt(data))
}

func Encrypt(msg []byte) []byte {
	aead, err := chacha20poly1305.NewX(config.MasterPass[:])
	if err != nil {
		panic(err)
	}

	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(msg)+aead.Overhead())
	rand.Read(nonce)

	// Encrypt the message and append the ciphertext to the nonce.
	return aead.Seal(nonce, nonce, msg, nil)
}

func Decrypt(msg []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(config.MasterPass[:])
	if err != nil {
		panic(err)
	}

	if len(msg) < aead.NonceSize() {
		panic("ciphertext too short")
	}

	// Split nonce and ciphertext.
	nonce, ciphertext := msg[:aead.NonceSize()], msg[aead.NonceSize():]

	// Decrypt the message and check it wasn't tampered with.
	decrypted, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return []byte{}, err
	}

	return decrypted, nil
}


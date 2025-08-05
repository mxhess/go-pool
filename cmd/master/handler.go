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
        "encoding/binary"
        "encoding/hex"
        "go-pool/config"
        "go-pool/logger"
        "go-pool/serializer"
        "go-pool/util"
        "io"
        "math"
        "net"
        "sync"
)

const Overhead = 40

// Connection tracking - we'll keep this simple in-memory tracking
// since it's just for current connections, not persistent data
var (
        numConns   = make(map[uint64]uint32)
        numConnsMu sync.RWMutex
)

func HandleSlave(conn net.Conn) {
        var connId uint64 = util.RandomUint64()

        // Cleanup function for when connection closes
        cleanup := func() {
                conn.Close()
                numConnsMu.Lock()
                delete(numConns, connId)
                numConnsMu.Unlock()
        }

        for {
                lenBuf := make([]byte, 2+Overhead)
                _, err := io.ReadFull(conn, lenBuf)
                if err != nil {
                        logger.Warn(err)
                        cleanup()
                        return
                }
                lenBuf, err = Decrypt(lenBuf)
                if err != nil {
                        logger.Warn(err)
                        cleanup()
                        return
                }
                len := int(lenBuf[0]) | (int(lenBuf[1]) << 8)

                // read the actual message
                buf := make([]byte, len+Overhead)
                _, err = io.ReadFull(conn, buf)
                if err != nil {
                        logger.Warn(err)
                        cleanup()
                        return
                }
                buf, err = Decrypt(buf)
                if err != nil {
                        logger.Warn(err)
                        cleanup()
                        return
                }
                logger.NetDev("Received message:", hex.EncodeToString(buf))
                OnMessage(buf, connId)
        }
}

func SendToConn(conn net.Conn, data []byte) {
        var dataLenBin = make([]byte, 0, 2)
        dataLenBin = binary.LittleEndian.AppendUint16(dataLenBin, uint16(len(data)))
        conn.Write(Encrypt(dataLenBin))
        conn.Write(Encrypt(data))
}

func OnMessage(msg []byte, connId uint64) {
        d := serializer.Deserializer{
                Data: msg,
        }
        if d.Error != nil {
                logger.Error(d.Error)
                return
        }

        packet := d.ReadUint8()

        switch packet {
        case 0: // Share Found packet
                numShares := uint32(d.ReadUvarint())
                wallet := d.ReadString()
                workerID := d.ReadString() // Read worker ID
                diff := d.ReadUvarint()

                if d.Error != nil {
                        logger.Error(d.Error)
                        return
                }

                // Worker activity is now tracked in SQLite via the share submission
                // No need to update Stats
                OnShareFound(wallet, diff, numShares, workerID)

        case 1: // Block Found packet
                if config.Cfg.UseP2Pool {
                        logger.Error("received Block Found packet; is using P2Pool")
                        return
                }

                height := d.ReadUvarint()
                reward := d.ReadUvarint()
                hash := hex.EncodeToString(d.ReadFixedByteArray(32))

                if d.Error != nil {
                        logger.Error(d.Error)
                        return
                }

                logger.Info("Found block height", height, "reward", float64(reward)/math.Pow10(config.Cfg.Atomic), "hash", hash)
                OnBlockFound(height, reward, hash)

        case 2: // Stats packet - just track connection counts
                conns := uint32(d.ReadUvarint())

                if d.Error != nil {
                        logger.Error(d.Error)
                        return
                }

                // Just update connection tracking
                numConnsMu.Lock()
                numConns[connId] = conns
                numConnsMu.Unlock()

                // Worker count is now calculated from SQLite when needed
                // No need to update Stats.Workers

        case 3: // P2Pool Share Found
                if !config.Cfg.UseP2Pool {
                        logger.Error("received P2Pool Share Found packet; is not using P2Pool")
                        return
                }

                height := d.ReadUvarint()
                OnP2PoolShareFound(height)

        default:
                logger.Error("unknown packet type", packet)
                return
        }
}

// GetTotalWorkers calculates total workers from connection tracking
// This is only used if you need real-time connection count
func GetTotalWorkers() uint32 {
        numConnsMu.RLock()
        defer numConnsMu.RUnlock()
        
        var total uint32
        for _, v := range numConns {
                total += v
        }
        return total
}


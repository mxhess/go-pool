


// daemon_pool.go - Connection pool with TTL for GSLB load distribution
package main

import (
    "fmt"
    "strings"
    "sync"
    "time"
    "go-pool/logger"
    "github.com/mxhess/go-salvium/rpc"
    "github.com/mxhess/go-salvium/rpc/daemon"
)

type PooledConnection struct {
    client    *daemon.Client
    rpc       *rpc.Client
    createdAt time.Time
}

type DaemonPool struct {
    url         string
    mu          sync.Mutex
    connections []*PooledConnection
    size        int
    maxAge      time.Duration
    current     int
}

// NewDaemonPool creates a pool of daemon connections
func NewDaemonPool(url string, size int, maxAge time.Duration) (*DaemonPool, error) {
    if size <= 0 {
        size = 5 // sensible default
    }
    if maxAge <= 0 {
        maxAge = 5 * time.Minute // connections refresh every 5 min
    }
    
    pool := &DaemonPool{
        url:         url,
        connections: make([]*PooledConnection, 0, size),
        size:        size,
        maxAge:      maxAge,
    }
    
    // Pre-populate the pool
    for i := 0; i < size; i++ {
        conn, err := pool.createConnection()
        if err != nil {
            logger.Warn("Failed to create connection", i, ":", err)
            continue
        }
        pool.connections = append(pool.connections, conn)
    }
    
    if len(pool.connections) == 0 {
        return nil, fmt.Errorf("failed to create any connections")
    }
    
    // Start the recycler
    go pool.recycleLoop()
    
    logger.Info("Daemon pool initialized with", len(pool.connections), "connections to", url)
    return pool, nil
}

func (p *DaemonPool) createConnection() (*PooledConnection, error) {
    // Each new connection hits GSLB, potentially getting a different backend
    rpcClient, err := rpc.NewClient(p.url)
    if err != nil {
        return nil, err
    }
    
    return &PooledConnection{
        client:    daemon.NewClient(rpcClient),
        rpc:       rpcClient,
        createdAt: time.Now(),
    }, nil
}

// Get returns the next connection in the pool (round-robin)
func (p *DaemonPool) Get() *daemon.Client {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    if len(p.connections) == 0 {
        return nil
    }
    
    // Round-robin through connections
    p.current = (p.current + 1) % len(p.connections)
    conn := p.connections[p.current]
    
    // Check if connection is too old
    if time.Since(conn.createdAt) > p.maxAge {
        // Try to replace it
        if newConn, err := p.createConnection(); err == nil {
            // oldConn.rpc.Close() // RPC client might not have Close
            p.connections[p.current] = newConn
            conn = newConn
            logger.Debug("Replaced aged connection")
        }
    }
    
    return conn.client
}

// recycleLoop gradually replaces connections to maintain GSLB distribution
func (p *DaemonPool) recycleLoop() {
    // Recycle one connection every (maxAge / pool size)
    // This spreads out the reconnections
    recyclePeriod := p.maxAge / time.Duration(p.size)
    if recyclePeriod < 30*time.Second {
        recyclePeriod = 30 * time.Second
    }
    
    ticker := time.NewTicker(recyclePeriod)
    defer ticker.Stop()
    
    recycleIndex := 0
    
    for range ticker.C {
        p.mu.Lock()
        
        if len(p.connections) > 0 {
            // Replace the oldest connection
            // oldConn := p.connections[recycleIndex]
            
            if newConn, err := p.createConnection(); err == nil {
                p.connections[recycleIndex] = newConn
                // oldConn.rpc.Close() // RPC client might not have Close
                logger.Debug("Recycled connection", recycleIndex)
            } else {
                logger.Warn("Failed to recycle connection:", err)
            }
            
            recycleIndex = (recycleIndex + 1) % len(p.connections)
        }
        
        p.mu.Unlock()
    }
}

// ExecuteWithRetry runs a function with automatic failover
func (p *DaemonPool) ExecuteWithRetry(fn func(*daemon.Client) error) error {
    attempts := 0
    maxAttempts := len(p.connections) + 1
    
    for attempts < maxAttempts {
        client := p.Get()
        if client == nil {
            return fmt.Errorf("no available connections")
        }
        
        err := fn(client)
        if err == nil {
            return nil // Success!
        }
        
        // Check if it's a connection error
        if isConnectionError(err) {
            logger.Warn("Connection error, trying next:", err)
            attempts++
            continue
        }
        
        // Non-connection error, return immediately
        return err
    }
    
    return fmt.Errorf("all connections failed")
}

// Helper to detect connection errors
func isConnectionError(err error) bool {
    if err == nil {
        return false
    }
    errStr := err.Error()
    return strings.Contains(errStr, "connection") ||
           strings.Contains(errStr, "timeout") ||
           strings.Contains(errStr, "refused") ||
           strings.Contains(errStr, "EOF")
}

// Close gracefully shuts down the pool
func (p *DaemonPool) Close() {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    // Just clear the connections, let GC handle cleanup
    p.connections = nil
}



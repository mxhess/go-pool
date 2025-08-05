

// verify_queue.go - Async verification queue with monitoring
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "sync"
    "time"
    "go-pool/logger"
    "github.com/mxhess/go-salvium/rpc/daemon"
    _ "modernc.org/sqlite"
)

type VerifyQueue struct {
    db              *sql.DB
    workers         int
    stopChan        chan bool
    stats           QueueStats
    statsMu         sync.RWMutex
    alertThreshold  time.Duration
    daemonPool      *DaemonPool  // Use the shared daemon pool
}

type QueueStats struct {
    Pending         int
    Processing      int
    Verified        int64
    Failed          int64
    AvgVerifyTime   time.Duration
    MaxVerifyTime   time.Duration
    QueueDepth      int
    LastAlert       time.Time
}

type QueuedVerification struct {
    ID              int64
    ShareData       string  // JSON encoded share
    ConnectionID    uint64
    SubmittedAt     time.Time
    StartedAt       *time.Time
    CompletedAt     *time.Time
    Status          string  // 'pending', 'processing', 'verified', 'failed'
    RetryCount      int
    NodeUsed        string
    VerifyDuration  *int64  // milliseconds
}

// Remove all the NodePool code - we'll use DaemonPool instead

func NewVerifyQueue(dbPath string, workers int, pool *DaemonPool) (*VerifyQueue, error) {
    // Better connection string for concurrent access
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, err
    }
    
    // Enable WAL mode and set busy timeout
    pragmas := []string{
        "PRAGMA journal_mode=WAL",
        "PRAGMA busy_timeout=5000",
        "PRAGMA synchronous=NORMAL",
        "PRAGMA cache_size=-20000",
        "PRAGMA temp_store=MEMORY",
    }
    
    for _, pragma := range pragmas {
        if _, err := db.Exec(pragma); err != nil {
            return nil, fmt.Errorf("failed to set %s: %w", pragma, err)
        }
    }
    
    // Set connection pool for write-heavy workload
    db.SetMaxOpenConns(1)    // SQLite only allows 1 writer
    db.SetMaxIdleConns(1)
    db.SetConnMaxLifetime(0)
    
    // Create schema
    schema := `
    CREATE TABLE IF NOT EXISTS verify_queue (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        share_data TEXT NOT NULL,
        connection_id INTEGER NOT NULL,
        submitted_at INTEGER NOT NULL,
        started_at INTEGER,
        completed_at INTEGER,
        status TEXT DEFAULT 'pending',
        retry_count INTEGER DEFAULT 0,
        node_used TEXT,
        verify_duration INTEGER,
        created_at INTEGER DEFAULT (strftime('%s', 'now'))
    );
    
    CREATE INDEX IF NOT EXISTS idx_status ON verify_queue(status);
    CREATE INDEX IF NOT EXISTS idx_submitted ON verify_queue(submitted_at);
    
    -- Metrics table for monitoring
    CREATE TABLE IF NOT EXISTS verify_metrics (
        timestamp INTEGER PRIMARY KEY,
        pending_count INTEGER,
        processing_count INTEGER,
        avg_duration_ms INTEGER,
        max_duration_ms INTEGER,
        verified_total INTEGER,
        failed_total INTEGER
    );
    `
    
    if _, err := db.Exec(schema); err != nil {
        return nil, err
    }
    
    vq := &VerifyQueue{
        db:             db,
        workers:        workers,
        stopChan:       make(chan bool),
        alertThreshold: 2 * time.Second,
        daemonPool:     pool,
    }
    
    // Start workers
    for i := 0; i < workers; i++ {
        go vq.worker(i)
    }
    
    // Start monitoring
    go vq.monitorLoop()
    
    // Start metrics collector
    go vq.metricsLoop()
    
    return vq, nil
}

func (vq *VerifyQueue) Submit(share ShareToVerify) error {
    data, err := json.Marshal(share)
    if err != nil {
        return err
    }
    
    _, err = vq.db.Exec(`
        INSERT INTO verify_queue (share_data, connection_id, submitted_at)
        VALUES (?, ?, ?)
    `, string(data), share.ConnectionID, time.Now().UnixNano())
    
    return err
}

func (vq *VerifyQueue) worker(id int) {
    logger.Info("Verify worker", id, "started")
    
    for {
        select {
        case <-vq.stopChan:
            return
        default:
            vq.processNext()
            time.Sleep(10 * time.Millisecond)
        }
    }
}

func (vq *VerifyQueue) processNext() {
    tx, err := vq.db.Begin()
    if err != nil {
        logger.Error("Failed to begin transaction:", err)
        return
    }
    defer tx.Rollback()
    
    // Get next pending item - scan as int64 for both fields
    var item QueuedVerification
    var submittedAtNano int64
    var connectionIDInt int64
    err = tx.QueryRow(`
        SELECT id, share_data, connection_id, submitted_at
        FROM verify_queue
        WHERE status = 'pending'
        ORDER BY submitted_at ASC
        LIMIT 1
    `).Scan(&item.ID, &item.ShareData, &connectionIDInt, &submittedAtNano)
    
    if err == sql.ErrNoRows {
        return
    }
    if err != nil {
        logger.Error("Failed to get next item:", err)
        return
    }
    
    // Convert from storage format back to original types
    item.SubmittedAt = time.Unix(0, submittedAtNano)
    item.ConnectionID = uint64(connectionIDInt) // This will wrap negative values back to original uint64
    
    // Mark as processing
    startTime := time.Now()
    _, err = tx.Exec(`
        UPDATE verify_queue 
        SET status = 'processing', started_at = ?
        WHERE id = ?
    `, startTime.UnixNano(), item.ID)
    
    if err != nil {
        logger.Error("Failed to update status:", err)
        return
    }
    
    if err := tx.Commit(); err != nil {
        logger.Error("Failed to commit:", err)
        return
    }
    
    // Unmarshal share data
    var share ShareToVerify
    if err := json.Unmarshal([]byte(item.ShareData), &share); err != nil {
        logger.Error("Failed to unmarshal share:", err)
        vq.markFailed(item.ID, "invalid_data")
        return
    }
    
    // Verify the share using the daemon pool
    var calcPow string
    err = vq.daemonPool.ExecuteWithRetry(func(client *daemon.Client) error {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        result, err := client.CalcPow(ctx, share.CalcPowParams)
        if err != nil {
            return err
        }
        calcPow = result
        return nil
    })
    
    duration := time.Since(startTime)
    
    if err != nil {
        logger.Error("CalcPow failed:", err)
        vq.markFailed(item.ID, "calc_pow_error")
        return
    }
    
    // Check if valid
    if calcPow != share.ExpectedResult {
        logger.Warn("Invalid share detected!")
        vq.markFailed(item.ID, "invalid_pow")
        // TODO: Update connection trust score
        return
    }
    
    // Mark as verified
    _, err = vq.db.Exec(`
        UPDATE verify_queue 
        SET status = 'verified', 
            completed_at = ?,
            verify_duration = ?
        WHERE id = ?
    `, time.Now().UnixNano(), duration.Milliseconds(), item.ID)
    
    if err != nil {
        logger.Error("Failed to mark verified:", err)
    }
    
    // Check if verification took too long
    if duration > vq.alertThreshold {
        vq.alert("Slow verification", fmt.Sprintf(
            "Share verification took %s (threshold: %s)",
            duration, vq.alertThreshold,
        ))
    }
    
    // Update stats
    vq.updateStats(duration)
}

func (vq *VerifyQueue) monitorLoop() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        var pending, processing int
        
        // Check queue depth
        err := vq.db.QueryRow(`
            SELECT COUNT(*) FROM verify_queue WHERE status = 'pending'
        `).Scan(&pending)
        
        if err == nil && pending > 1000 {
            vq.alert("Queue depth warning", fmt.Sprintf(
                "Verification queue has %d pending items", pending,
            ))
        }
        
        // Check processing count
        err = vq.db.QueryRow(`
            SELECT COUNT(*) FROM verify_queue WHERE status = 'processing'
        `).Scan(&processing)
        
        // Check oldest pending
        var oldestNano sql.NullInt64
        err = vq.db.QueryRow(`
            SELECT MIN(submitted_at) FROM verify_queue WHERE status = 'pending'
        `).Scan(&oldestNano)
        
        if err == nil && oldestNano.Valid && oldestNano.Int64 > 0 {
            oldest := time.Unix(0, oldestNano.Int64)
            age := time.Since(oldest)
            if age > 30*time.Second {
                vq.alert("Queue backup", fmt.Sprintf(
                    "Oldest pending verification is %s old", age,
                ))
            }
        }
        
        // Update current stats
        vq.statsMu.Lock()
        vq.stats.Pending = pending
        vq.stats.Processing = processing
        vq.statsMu.Unlock()
    }
}

func (vq *VerifyQueue) alert(title, message string) {
    vq.statsMu.Lock()
    lastAlert := vq.stats.LastAlert
    vq.stats.LastAlert = time.Now()
    vq.statsMu.Unlock()
    
    // Rate limit alerts
    if time.Since(lastAlert) < 1*time.Minute {
        return
    }
    
    logger.Error("ALERT:", title, "-", message)
    // TODO: Send to monitoring system, Slack, etc.
}

func (vq *VerifyQueue) GetStats() QueueStats {
    vq.statsMu.RLock()
    defer vq.statsMu.RUnlock()
    return vq.stats
}

// Cleanup old verified entries
func (vq *VerifyQueue) cleanup() {
    cutoff := time.Now().Add(-1 * time.Hour).UnixNano()
    _, err := vq.db.Exec(`
        DELETE FROM verify_queue 
        WHERE status IN ('verified', 'failed') 
        AND completed_at < ?
    `, cutoff)
    
    if err != nil {
        logger.Error("Cleanup failed:", err)
    }
}

type ShareToVerify struct {
    ConnectionID   uint64
    CalcPowParams  daemon.CalcPowParameters
    ExpectedResult string
    ShareDiff      uint64
    MinerAddr      string
    WorkerID       string
}

// Add these missing methods:

func (vq *VerifyQueue) markFailed(id int64, reason string) {
    _, err := vq.db.Exec(`
        UPDATE verify_queue 
        SET status = 'failed', 
            completed_at = ?,
            node_used = ?
        WHERE id = ?
    `, time.Now().UnixNano(), reason, id)
    
    if err != nil {
        logger.Error("Failed to mark as failed:", err)
    }
    
    vq.statsMu.Lock()
    vq.stats.Failed++
    vq.statsMu.Unlock()
}

func (vq *VerifyQueue) updateStats(duration time.Duration) {
    vq.statsMu.Lock()
    defer vq.statsMu.Unlock()
    
    vq.stats.Verified++
    
    // Update average
    if vq.stats.AvgVerifyTime == 0 {
        vq.stats.AvgVerifyTime = duration
    } else {
        // Running average
        vq.stats.AvgVerifyTime = (vq.stats.AvgVerifyTime + duration) / 2
    }
    
    // Update max
    if duration > vq.stats.MaxVerifyTime {
        vq.stats.MaxVerifyTime = duration
    }
}

func (vq *VerifyQueue) metricsLoop() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        vq.statsMu.RLock()
        stats := vq.stats
        vq.statsMu.RUnlock()
        
        // Store metrics
        _, err := vq.db.Exec(`
            INSERT INTO verify_metrics 
            (timestamp, pending_count, processing_count, avg_duration_ms, 
             max_duration_ms, verified_total, failed_total)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        `, time.Now().Unix(), stats.Pending, stats.Processing,
            stats.AvgVerifyTime.Milliseconds(), stats.MaxVerifyTime.Milliseconds(),
            stats.Verified, stats.Failed)
        
        if err != nil {
            logger.Error("Failed to store metrics:", err)
        }
        
        // Cleanup old entries periodically
        go vq.cleanup()
    }
}

// Stop gracefully shuts down the verify queue
func (vq *VerifyQueue) Stop() {
    close(vq.stopChan)
    vq.db.Close()
}


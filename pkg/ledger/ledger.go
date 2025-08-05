

package ledger

import (
        "database/sql"
	"go-pool/logger"
        "embed"
        "fmt"
        "log"
        "time"

        _ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL embed.FS

// LedgerDB wraps the SQLite database with mining pool operations
type LedgerDB struct {
        db *sql.DB
        readDB   *sql.DB
}

// NewLedgerDB creates a new mining pool ledger database
func NewLedgerDB(dbPath string) (*LedgerDB, error) {
    // Write connection (single connection for writes)
    connStr := fmt.Sprintf("file:%s?_pragma=foreign_keys(0)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-20000)&_pragma=temp_store(memory)", dbPath)
    
    db, err := sql.Open("sqlite", connStr)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }
    
    // Set write connection pool settings
    db.SetMaxOpenConns(1)    // Only ONE writer to prevent SQLITE_BUSY
    db.SetMaxIdleConns(1)
    db.SetConnMaxLifetime(0)
    
    // For now, use same connection for reads until we can set up true read replicas
    // This still helps because we're limiting write connections
    readDB := db
    
    ledger := &LedgerDB{
        db:     db,
        readDB: readDB,
    }
    
    // Test connection
    if err := db.Ping(); err != nil {
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }
    
    // Create tables if they don't exist
    if err := ledger.initializeSchema(); err != nil {
        return nil, fmt.Errorf("failed to initialize schema: %w", err)
    }
    
    log.Printf("✅ SQLite ledger initialized: %s", dbPath)
    return ledger, nil
}

// initializeSchema creates all tables and indexes
func (l *LedgerDB) initializeSchema() error {
        schema, err := schemaSQL.ReadFile("schema.sql")
        if err != nil {
                return fmt.Errorf("failed to read schema: %w", err)
        }

        if _, err := l.db.Exec(string(schema)); err != nil {
                return fmt.Errorf("failed to execute schema: %w", err)
        }

        return nil
}

// Close closes the database connection
func (l *LedgerDB) Close() error {
        if l.readDB != l.db {
                // If we have separate read connection, close it
                l.readDB.Close()
        }
        return l.db.Close()
}

// WithTransaction executes a function within a database transaction
func (l *LedgerDB) WithTransaction(fn func(*sql.Tx) error) error {
        tx, err := l.db.Begin()
        if err != nil {
                return fmt.Errorf("failed to begin transaction: %w", err)
        }

        defer func() {
                if err != nil {
                        if rollbackErr := tx.Rollback(); rollbackErr != nil {
                                log.Printf("Failed to rollback transaction: %v", rollbackErr)
                        }
                } else {
                        err = tx.Commit()
                }
        }()

        err = fn(tx)
        return err
}

// =============================================================================
// IDEMPOTENCY CHECKS - THE BUG KILLERS!
// =============================================================================

// IsTransferProcessed checks if a wallet transfer has already been processed
// THIS PREVENTS THE INFINITE LOOP BUG!
func (l *LedgerDB) IsTransferProcessed(txid string) (bool, error) {
        var count int
        err := l.readDB.QueryRow("SELECT COUNT(*) FROM processed_transfers WHERE txid = ?", txid).Scan(&count)
        if err != nil {
                return false, fmt.Errorf("failed to check transfer: %w", err)
        }
        return count > 0, nil
}

// MarkTransferProcessed records that a transfer has been processed
func (l *LedgerDB) MarkTransferProcessed(txid string, height uint64, amount uint64) error {
        _, err := l.db.Exec(`
                INSERT OR IGNORE INTO processed_transfers (txid, height, amount, processed_at) 
                VALUES (?, ?, ?, ?)`,
                txid, height, amount, time.Now().Unix())

        if err != nil {
                return fmt.Errorf("failed to mark transfer processed: %w", err)
        }
        return nil
}

// IsBlockProcessed checks if a block has already been processed
func (l *LedgerDB) IsBlockProcessed(height uint64) (bool, error) {
        var count int
        err := l.readDB.QueryRow(`
                SELECT COUNT(*) FROM blocks_found 
                WHERE height = ? AND status IN ('completed', 'orphaned')`, height).Scan(&count)

        if err != nil {
                return false, fmt.Errorf("failed to check block: %w", err)
        }
        return count > 0, nil
}

// =============================================================================
// BLOCK OPERATIONS
// =============================================================================

// CreateBlock adds a new block to the ledger
func (l *LedgerDB) CreateBlock(block BlockFound) error {
        _, err := l.db.Exec(`
                INSERT OR IGNORE INTO blocks_found 
                (height, hash, reward_total, timestamp, found_at, status) 
                VALUES (?, ?, ?, ?, ?, ?)`,
                block.Height, block.Hash, block.RewardTotal,
                block.Timestamp, block.FoundAt, block.Status)

        if err != nil {
                return fmt.Errorf("failed to create block: %w", err)
        }
        return nil
}

// GetBlock retrieves a block by height
func (l *LedgerDB) GetBlock(height uint64) (*BlockFound, error) {
        var block BlockFound
        err := l.readDB.QueryRow(`
                SELECT height, hash, reward_total, timestamp, found_at, processed_at, status
                FROM blocks_found WHERE height = ?`, height).Scan(
                &block.Height, &block.Hash, &block.RewardTotal,
                &block.Timestamp, &block.FoundAt, &block.ProcessedAt, &block.Status)

        if err != nil {
                if err == sql.ErrNoRows {
                        return nil, nil
                }
                return nil, fmt.Errorf("failed to get block: %w", err)
        }
        return &block, nil
}

// MarkBlockProcessed marks a block as completed
func (l *LedgerDB) MarkBlockProcessed(height uint64) error {
        now := time.Now().Unix()
        _, err := l.db.Exec(`
                UPDATE blocks_found 
                SET processed_at = ?, status = 'completed' 
                WHERE height = ?`, now, height)

        if err != nil {
                return fmt.Errorf("failed to mark block processed: %w", err)
        }
        return nil
}

// =============================================================================
// SHARE OPERATIONS
// =============================================================================

// AddShare records a share submission
func (l *LedgerDB) AddShare(share Share) error {
        _, err := l.db.Exec(`
                INSERT INTO shares 
                (block_height, miner_address, worker_id, difficulty, timestamp) 
                VALUES (?, ?, ?, ?, ?)`,
                share.BlockHeight, share.MinerAddr, share.WorkerID,
                share.Difficulty, share.Timestamp)

        if err != nil {
                return fmt.Errorf("failed to add share: %w", err)
        }
        return nil
}

// GetSharesInWindow retrieves shares within PPLNS window
func (l *LedgerDB) GetSharesInWindow(windowStart int64) ([]Share, error) {
    start := time.Now()
    logger.Info("GetSharesInWindow: Starting query...")
    
    rows, err := l.readDB.Query(`
        SELECT id, block_height, miner_address, worker_id, difficulty, timestamp
        FROM shares 
        WHERE timestamp >= ?
        ORDER BY timestamp DESC`, windowStart)

    if err != nil {
        return nil, fmt.Errorf("failed to get shares: %w", err)
    }
    defer rows.Close()

    var shares []Share
    count := 0
    for rows.Next() {
        var share Share
        err := rows.Scan(&share.ID, &share.BlockHeight, &share.MinerAddr,
                &share.WorkerID, &share.Difficulty, &share.Timestamp)
        if err != nil {
            return nil, fmt.Errorf("failed to scan share: %w", err)
        }
        shares = append(shares, share)
        count++
    }
    
    logger.Info("GetSharesInWindow: Got", count, "shares in", time.Since(start))
    return shares, nil
}

// =============================================================================
// DISTRIBUTION OPERATIONS
// =============================================================================

// CreateDistribution records reward distribution for a block
func (l *LedgerDB) CreateDistribution(dist Distribution) error {
        _, err := l.db.Exec(`
                INSERT OR REPLACE INTO distributions 
                (block_height, miner_address, shares_contributed, percentage, 
                 reward_gross, reward_net, calculated_at) 
                VALUES (?, ?, ?, ?, ?, ?, ?)`,
                dist.BlockHeight, dist.MinerAddr, dist.SharesContrib,
                dist.Percentage, dist.RewardGross, dist.RewardNet, dist.CalculatedAt)

        if err != nil {
                return fmt.Errorf("failed to create distribution: %w", err)
        }
        return nil
}

// GetDistributionsForBlock retrieves all distributions for a specific block
func (l *LedgerDB) GetDistributionsForBlock(height uint64) ([]Distribution, error) {
        rows, err := l.readDB.Query(`
                SELECT id, block_height, miner_address, shares_contributed, 
                       percentage, reward_gross, reward_net, calculated_at
                FROM distributions 
                WHERE block_height = ?`, height)

        if err != nil {
                return nil, fmt.Errorf("failed to get distributions: %w", err)
        }
        defer rows.Close()

        var distributions []Distribution
        for rows.Next() {
                var dist Distribution
                err := rows.Scan(&dist.ID, &dist.BlockHeight, &dist.MinerAddr,
                        &dist.SharesContrib, &dist.Percentage, &dist.RewardGross,
                        &dist.RewardNet, &dist.CalculatedAt)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan distribution: %w", err)
                }
                distributions = append(distributions, dist)
        }
        return distributions, nil
}

// =============================================================================
// PAYMENT OPERATIONS
// =============================================================================

// CreatePayment queues a payment for a miner
func (l *LedgerDB) CreatePayment(payment Payment) error {
        _, err := l.db.Exec(`
                INSERT INTO payments 
                (block_height, recipient_address, amount, fee, status, created_at) 
                VALUES (?, ?, ?, ?, ?, ?)`,
                payment.BlockHeight, payment.RecipientAddr, payment.Amount,
                payment.Fee, payment.Status, payment.CreatedAt)

        if err != nil {
                return fmt.Errorf("failed to create payment: %w", err)
        }
        return nil
}

// GetPendingPayments retrieves all payments ready to be sent
func (l *LedgerDB) GetPendingPayments() ([]Payment, error) {
        rows, err := l.readDB.Query(`
                SELECT id, block_height, recipient_address, amount, fee, 
                       txid, status, created_at, sent_at, confirmed_at
                FROM payments 
                WHERE status = 'pending'
                ORDER BY created_at ASC`)

        if err != nil {
                return nil, fmt.Errorf("failed to get pending payments: %w", err)
        }
        defer rows.Close()

        var payments []Payment
        for rows.Next() {
                var payment Payment
                err := rows.Scan(&payment.ID, &payment.BlockHeight, &payment.RecipientAddr,
                        &payment.Amount, &payment.Fee, &payment.TxID, &payment.Status,
                        &payment.CreatedAt, &payment.SentAt, &payment.ConfirmedAt)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan payment: %w", err)
                }
                payments = append(payments, payment)
        }
        return payments, nil
}

// UpdatePaymentStatus updates a payment's status and transaction details
func (l *LedgerDB) UpdatePaymentStatus(paymentID uint64, status string, txid *string) error {
        var err error
        now := time.Now().Unix()

        if status == "sent" && txid != nil {
                _, err = l.db.Exec(`
                        UPDATE payments 
                        SET status = ?, txid = ?, sent_at = ?, updated_at = ?
                        WHERE id = ?`, status, *txid, now, now, paymentID)
        } else if status == "confirmed" {
                _, err = l.db.Exec(`
                        UPDATE payments 
                        SET status = ?, confirmed_at = ?, updated_at = ?
                        WHERE id = ?`, status, now, now, paymentID)
        } else {
                _, err = l.db.Exec(`
                        UPDATE payments 
                        SET status = ?, updated_at = ?
                        WHERE id = ?`, status, now, paymentID)
        }

        if err != nil {
                return fmt.Errorf("failed to update payment status: %w", err)
        }
        return nil
}

// =============================================================================
// BALANCE OPERATIONS
// =============================================================================

// GetBalance retrieves current balance for a miner
func (l *LedgerDB) GetBalance(minerAddr string) (*Balance, error) {
    var balance Balance
    err := l.readDB.QueryRow(`
        SELECT miner_address, balance_pending, balance_confirmed, total_paid, last_updated
        FROM balances WHERE miner_address = ?`, minerAddr).Scan(
        &balance.MinerAddr, &balance.BalancePending, &balance.BalanceConfirmed,
        &balance.TotalPaid, &balance.LastUpdated)

        if err != nil {
                if err == sql.ErrNoRows {
                        // Return zero balance for new miner
                        return &Balance{
                                MinerAddr:        minerAddr,
                                BalancePending:   0,
                                BalanceConfirmed: 0,
                                TotalPaid:        0,
                                LastUpdated:      time.Now().Unix(),
                        }, nil
                }
                return nil, fmt.Errorf("failed to get balance: %w", err)
        }
        return &balance, nil
}

// =============================================================================
// STATISTICS AND REPORTING
// =============================================================================

// GetRecentBlocks retrieves recently found blocks (returns BlockInfo for API)
func (l *LedgerDB) GetRecentBlocks(limit int) ([]BlockInfo, error) {
        rows, err := l.readDB.Query(`
                SELECT height, hash, reward_total, timestamp, status
                FROM blocks_found 
                ORDER BY height DESC 
                LIMIT ?`, limit)

        if err != nil {
                return nil, fmt.Errorf("failed to get recent blocks: %w", err)
        }
        defer rows.Close()

        var blocks []BlockInfo
        for rows.Next() {
                var block BlockInfo
                err := rows.Scan(&block.Height, &block.Hash, &block.RewardTotal,
                        &block.Timestamp, &block.Status)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan block: %w", err)
                }
                blocks = append(blocks, block)
        }
        return blocks, nil
}

// GetTopMiners retrieves top performing miners
func (l *LedgerDB) GetTopMiners(limit int) ([]TopMiner, error) {
        rows, err := l.readDB.Query(`SELECT * FROM top_miners_24h LIMIT ?`, limit)
        if err != nil {
                return nil, fmt.Errorf("failed to get top miners: %w", err)
        }
        defer rows.Close()

        var miners []TopMiner
        for rows.Next() {
                var miner TopMiner
                err := rows.Scan(&miner.MinerAddr, &miner.WorkerCount, &miner.AvgHashrate,
                        &miner.TotalShares, &miner.CountryName, &miner.CountryCode)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan miner: %w", err)
                }
                miners = append(miners, miner)
        }
        return miners, nil
}

// =============================================================================
// API SUPPORT METHODS
// =============================================================================

// GetMinerSharesInWindow gets all shares for a specific miner within a time window
func (l *LedgerDB) GetMinerSharesInWindow(minerAddr string, startTime int64) ([]Share, error) {
        rows, err := l.readDB.Query(`
                SELECT id, block_height, miner_address, worker_id, difficulty, timestamp
                FROM shares
                WHERE miner_address = ? AND timestamp >= ?
                ORDER BY timestamp DESC`, minerAddr, startTime)

        if err != nil {
                return nil, fmt.Errorf("failed to get miner shares: %w", err)
        }
        defer rows.Close()

        var shares []Share
        for rows.Next() {
                var s Share
                err := rows.Scan(&s.ID, &s.BlockHeight, &s.MinerAddr, &s.WorkerID, &s.Difficulty, &s.Timestamp)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan share: %w", err)
                }
                shares = append(shares, s)
        }

        return shares, rows.Err()
}

// GetActiveWorkers gets unique workers for a miner active since timestamp
func (l *LedgerDB) GetActiveWorkers(minerAddr string, since int64) ([]string, error) {
        rows, err := l.readDB.Query(`
                SELECT DISTINCT worker_id
                FROM shares
                WHERE miner_address = ? AND timestamp >= ?`, minerAddr, since)

        if err != nil {
                return nil, fmt.Errorf("failed to get active workers: %w", err)
        }
        defer rows.Close()

        var workers []string
        for rows.Next() {
                var worker string
                if err := rows.Scan(&worker); err != nil {
                        return nil, fmt.Errorf("failed to scan worker: %w", err)
                }
                workers = append(workers, worker)
        }

        return workers, rows.Err()
}

// GetMinerWithdrawals gets recent withdrawals for a miner
func (l *LedgerDB) GetMinerWithdrawals(minerAddr string, limit int) ([]Withdrawal, error) {
        rows, err := l.readDB.Query(`
                SELECT txid, amount, updated_at
                FROM payments
                WHERE recipient_address = ? AND status = 'sent' AND txid IS NOT NULL
                ORDER BY updated_at DESC
                LIMIT ?`, minerAddr, limit)

        if err != nil {
                return nil, fmt.Errorf("failed to get withdrawals: %w", err)
        }
        defer rows.Close()

        var withdrawals []Withdrawal
        for rows.Next() {
                var w Withdrawal
                var txid sql.NullString
                var timestamp sql.NullInt64

                err := rows.Scan(&txid, &w.Amount, &timestamp)
                if err != nil {
                        return nil, fmt.Errorf("failed to scan withdrawal: %w", err)
                }

                if txid.Valid {
                        w.TxID = txid.String
                }
                if timestamp.Valid {
                        w.Timestamp = timestamp.Int64
                }

                withdrawals = append(withdrawals, w)
        }

        return withdrawals, rows.Err()
}

// GetActiveMinerCount gets count of unique miners active since timestamp
func (l *LedgerDB) GetActiveMinerCount(since int64) (int, error) {
        var count int
        err := l.readDB.QueryRow(`
                SELECT COUNT(DISTINCT miner_address)
                FROM shares
                WHERE timestamp >= ?`, since).Scan(&count)

        if err != nil {
                return 0, fmt.Errorf("failed to get active miner count: %w", err)
        }
        return count, nil
}

// GetActiveWorkerCount gets count of unique worker combinations active since timestamp
func (l *LedgerDB) GetActiveWorkerCount(since int64) (int, error) {
        var count int
        err := l.readDB.QueryRow(`
                SELECT COUNT(DISTINCT miner_address || ':' || worker_id)
                FROM shares
                WHERE timestamp >= ?`, since).Scan(&count)

        if err != nil {
                return 0, fmt.Errorf("failed to get active worker count: %w", err)
        }
        return count, nil
}

// TopMinerAPI is the structure expected by the API with all miner stats
type TopMinerAPI struct {
        MinerAddr         string
        ShareCount        uint64   // Number of shares submitted
        SharesInWindow    uint64   // Number of shares in current PPLNS window
        WorkerCount       int      // Number of workers
        Percentage        float64  // Percentage of pool (based on PPLNS weight)
        LastShareTime     int64    // When they last submitted a share
}

// GetTopMinersForAPI returns top miners in the format expected by the API
func (l *LedgerDB) GetTopMinersForAPI(limit int) ([]TopMinerAPI, error) {
        // Get PPLNS window duration
        pplnsWindow := int64(21600) // 6 hours default
        windowStart := time.Now().Unix() - pplnsWindow

        // Get miners with shares in PPLNS window
        rows, err := l.readDB.Query(`
                SELECT 
                        miner_address,
                        COUNT(DISTINCT worker_id) as worker_count,
                        COUNT(*) as shares_in_window,
                        SUM(difficulty) as pplns_weight,
                        MAX(timestamp) as last_share_time
                FROM shares
                WHERE timestamp >= ?
                GROUP BY miner_address
                ORDER BY pplns_weight DESC
                LIMIT ?`, windowStart, limit)

        if err != nil {
                return nil, fmt.Errorf("failed to get top miners: %w", err)
        }
        defer rows.Close()

        var miners []TopMinerAPI
        var totalWeight uint64

        // First pass to collect data and total weight
        type tempMiner struct {
                addr           string
                workers        int
                sharesInWindow uint64
                pplnsWeight    uint64
                lastShareTime  int64
        }
        var tempMiners []tempMiner

        for rows.Next() {
                var tm tempMiner
                err := rows.Scan(&tm.addr, &tm.workers, &tm.sharesInWindow, &tm.pplnsWeight, &tm.lastShareTime)
                if err != nil {
                        return nil, err
                }
                tempMiners = append(tempMiners, tm)
                totalWeight += tm.pplnsWeight
        }

        // Now get total shares (all time) for each miner
        for _, tm := range tempMiners {
                // Get total share count for this miner
                var totalShares uint64
                err := l.readDB.QueryRow(`
                        SELECT COUNT(*) FROM shares WHERE miner_address = ?`, 
                        tm.addr).Scan(&totalShares)
                if err != nil {
                        totalShares = tm.sharesInWindow // Fallback
                }

                m := TopMinerAPI{
                        MinerAddr:      tm.addr,
                        ShareCount:     totalShares,
                        SharesInWindow: tm.sharesInWindow,
                        WorkerCount:    tm.workers,
                        Percentage:     0,
                        LastShareTime:  tm.lastShareTime,
                }

                // Calculate percentage based on PPLNS weight
                if totalWeight > 0 {
                        m.Percentage = float64(tm.pplnsWeight) / float64(totalWeight) * 100
                }

                miners = append(miners, m)
        }

        return miners, nil
}

// WorkerStats represents statistics for a single worker
type WorkerStats struct {
        WorkerID          string
        ShareCount        uint64  // Total shares submitted by this worker
        SharesInWindow    uint64  // Shares in current PPLNS window
        LastShareTime     int64   // When this worker last submitted a share
        Percentage        float64 // Percentage of miner's total shares
        Hashrate          float64 // Worker's hashrate
}

// GetMinerWorkerStats gets per-worker statistics for a specific miner
func (l *LedgerDB) GetMinerWorkerStats(minerAddr string, windowStart int64) ([]WorkerStats, error) {
        // Get stats for each worker with hashrate calculation
        rows, err := l.readDB.Query(`
                SELECT 
                        worker_id,
                        COUNT(*) as total_shares,
                        SUM(CASE WHEN timestamp >= ? THEN 1 ELSE 0 END) as shares_in_window,
                        SUM(CASE WHEN timestamp >= ? THEN difficulty ELSE 0 END) as window_difficulty,
                        MAX(timestamp) as last_share_time
                FROM shares
                WHERE miner_address = ?
                GROUP BY worker_id
                ORDER BY total_shares DESC`, windowStart, windowStart, minerAddr)

        if err != nil {
                return nil, fmt.Errorf("failed to get worker stats: %w", err)
        }
        defer rows.Close()

        var workers []WorkerStats
        var totalShares uint64

        // First pass to get total
        type tempWorker struct {
                id             string
                totalShares    uint64
                sharesInWindow uint64
                windowDiff     uint64
                lastShareTime  int64
        }
        var tempWorkers []tempWorker

        for rows.Next() {
                var tw tempWorker
                err := rows.Scan(&tw.id, &tw.totalShares, &tw.sharesInWindow, &tw.windowDiff, &tw.lastShareTime)
                if err != nil {
                        return nil, err
                }
                tempWorkers = append(tempWorkers, tw)
                totalShares += tw.totalShares
        }

        // Calculate percentages and hashrates
        windowDuration := time.Now().Unix() - windowStart
        if windowDuration <= 0 {
                windowDuration = 1
        }

        for _, tw := range tempWorkers {
                w := WorkerStats{
                        WorkerID:       tw.id,
                        ShareCount:     tw.totalShares,
                        SharesInWindow: tw.sharesInWindow,
                        LastShareTime:  tw.lastShareTime,
                        Percentage:     0,
                        Hashrate:       float64(tw.windowDiff) / float64(windowDuration), // H/s
                }
                if totalShares > 0 {
                        w.Percentage = float64(tw.totalShares) / float64(totalShares) * 100
                }
                workers = append(workers, w)
        }

        return workers, nil
}

// Alias methods for API compatibility
var GetBalance = (*LedgerDB).GetBalance
var GetRecentBlocks = (*LedgerDB).GetRecentBlocks
var GetTopMiners = (*LedgerDB).GetTopMiners

// Export the conn field for direct access if needed
func (l *LedgerDB) Conn() *sql.DB {
        return l.db
}

func (l *LedgerDB) StorePoolStats(stats PoolStats) error {
    query := `
        INSERT OR REPLACE INTO pool_stats_history 
        (timestamp, pool_hashrate, network_hashrate, network_difficulty, 
         connected_miners, connected_workers, round_shares, blocks_found)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `
    _, err := l.db.Exec(query, 
        stats.Timestamp, 
        int64(stats.TotalHashrate),      // Convert to int64
        int64(stats.NetworkHashrate),    // Convert to int64
        int64(stats.NetworkDifficulty),  // Convert to int64
        stats.ConnectedMiners,
        stats.ConnectedWorkers,
        0,  // RoundShares
        stats.BlocksFound24h,
    )
    return err
}

// GetPoolStatsHistory retrieves pool stats for charting
func (l *LedgerDB) GetPoolStatsHistory(hours int) ([]PoolStats, error) {
    since := time.Now().Unix() - int64(hours*3600)
    query := `
        SELECT timestamp, pool_hashrate, network_hashrate, network_difficulty,
               connected_miners, connected_workers, round_shares, blocks_found
        FROM pool_stats_history
        WHERE timestamp > ?
        ORDER BY timestamp ASC
    `
    rows, err := l.readDB.Query(query, since)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var stats []PoolStats
    for rows.Next() {
        var s PoolStats
        var roundShares uint64
        var blocksFound int
        
        err := rows.Scan(&s.Timestamp, &s.TotalHashrate, &s.NetworkHashrate,
            &s.NetworkDifficulty, &s.ConnectedMiners, &s.ConnectedWorkers,
            &roundShares, &blocksFound)
        if err != nil {
            return nil, err
        }
        
        // Map to the models.go fields
        s.BlocksFound24h = blocksFound
        
        stats = append(stats, s)
    }
    return stats, rows.Err()
}

// StoreMinerHashrate stores miner hashrate history
func (l *LedgerDB) StoreMinerHashrate(minerAddr string, hashrate5m, hashrate15m, hashrate1h float64, workerCount int) error {
    query := `
        INSERT OR REPLACE INTO miner_hashrate_history
        (miner_addr, timestamp, hashrate_5m, hashrate_15m, hashrate_1h, worker_count)
        VALUES (?, ?, ?, ?, ?, ?)
    `
    _, err := l.db.Exec(query, minerAddr, time.Now().Unix(), 
        hashrate5m, hashrate15m, hashrate1h, workerCount)
    return err
}

// GetMinerHashrateHistory retrieves miner hashrate history
func (l *LedgerDB) GetMinerHashrateHistory(minerAddr string, hours int) ([]MinerHashratePoint, error) {
    since := time.Now().Unix() - int64(hours*3600)
    query := `
        SELECT timestamp, hashrate_5m
        FROM miner_hashrate_history
        WHERE miner_addr = ? AND timestamp > ?
        ORDER BY timestamp ASC
    `
    rows, err := l.readDB.Query(query, minerAddr, since)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var points []MinerHashratePoint
    for rows.Next() {
        var p MinerHashratePoint
        err := rows.Scan(&p.Timestamp, &p.Hashrate)
        if err != nil {
            return nil, err
        }
        points = append(points, p)
    }
    return points, rows.Err()
}

// CleanupOldChartData removes chart data older than specified days
func (l *LedgerDB) CleanupOldChartData(days int) error {
    cutoff := time.Now().Unix() - int64(days*24*3600)
    
    queries := []string{
        "DELETE FROM pool_stats_history WHERE timestamp < ?",
        "DELETE FROM miner_hashrate_history WHERE timestamp < ?",
        "DELETE FROM worker_hashrate_history WHERE timestamp < ?",
    }
    
    for _, query := range queries {
        if _, err := l.db.Exec(query, cutoff); err != nil {
            return err
        }
    }
    return nil
}

// GetRoundShares calculates shares since last block
func (l *LedgerDB) GetRoundShares() (uint64, error) {
    // Get timestamp of last found block
    var lastBlockTime sql.NullInt64
    err := l.readDB.QueryRow(`
        SELECT MAX(timestamp) FROM blocks_found WHERE status = 'completed'
    `).Scan(&lastBlockTime)
    
    if err != nil || !lastBlockTime.Valid {
        // No blocks found yet, count all shares
        lastBlockTime.Int64 = 0
    }

    // Sum shares since last block
    var roundShares sql.NullInt64
    err = l.readDB.QueryRow(`
        SELECT COALESCE(SUM(difficulty), 0) FROM shares WHERE timestamp > ?
    `, lastBlockTime.Int64).Scan(&roundShares)
    
    if err != nil {
        return 0, err
    }
    
    return uint64(roundShares.Int64), nil
}

// AddBlockFound adds a found block to the database
func (l *LedgerDB) AddBlockFound(block *BlockFound) error {
    _, err := l.db.Exec(`
        INSERT OR IGNORE INTO blocks_found 
        (height, hash, reward_total, timestamp, found_at, status) 
        VALUES (?, ?, ?, ?, ?, ?)`,
        block.Height, block.Hash, block.RewardTotal,
        block.Timestamp, time.Now().Unix(), block.Status)
    
    if err != nil {
        return fmt.Errorf("failed to add found block: %w", err)
    }
    return nil
}

// DebugStats returns database statistics for debugging
func (l *LedgerDB) DebugStats() {
    var count int
    var oldestTimestamp int64
    
    // Count total shares
    err := l.readDB.QueryRow("SELECT COUNT(*) FROM shares").Scan(&count)
    if err == nil {
        log.Printf("Total shares in database: %d", count)
    }
    
    // Get oldest share
    err = l.readDB.QueryRow("SELECT MIN(timestamp) FROM shares").Scan(&oldestTimestamp)
    if err == nil && oldestTimestamp > 0 {
        age := time.Since(time.Unix(oldestTimestamp, 0))
        log.Printf("Oldest share age: %v", age)
    }
    
    // Check indexes
    rows, err := l.readDB.Query(`
        SELECT name, sql FROM sqlite_master 
        WHERE type = 'index' AND tbl_name = 'shares'
    `)
    if err == nil {
        defer rows.Close()
        log.Printf("Indexes on shares table:")
        for rows.Next() {
            var name, sql string
            rows.Scan(&name, &sql)
            log.Printf("  - %s: %s", name, sql)
        }
    }
}

// GetBlockchainInfo retrieves current blockchain state from SQLite
func (l *LedgerDB) GetBlockchainInfo() (height uint64, blockReward uint64, difficulty uint64, err error) {
	row := l.db.QueryRow(`
		SELECT height, block_reward, difficulty 
		FROM blockchain_info 
		WHERE id = 1
	`)
	
	err = row.Scan(&height, &blockReward, &difficulty)
	if err != nil {
		// Return defaults if no data yet
		if err == sql.ErrNoRows {
			return 0, 0, 1, nil
		}
		return 0, 0, 0, err
	}
	
	return height, blockReward, difficulty, nil
}

// UpdateBlockchainInfo updates the blockchain state in SQLite
func (l *LedgerDB) UpdateBlockchainInfo(height, blockReward, difficulty uint64) error {
	_, err := l.db.Exec(`
		UPDATE blockchain_info 
		SET height = ?, block_reward = ?, difficulty = ?, updated_at = ?
		WHERE id = 1
	`, height, blockReward, difficulty, time.Now().Unix())
	
	return err
}


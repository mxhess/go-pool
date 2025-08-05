

package ledger

import (
	"database/sql"
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
}

// NewLedgerDB creates a new mining pool ledger database
func NewLedgerDB(dbPath string) (*LedgerDB, error) {
	// Connection string with performance optimizations
	connStr := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-20000)&_pragma=temp_store(memory)", dbPath)
	
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings for optimal performance
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	ledger := &LedgerDB{db: db}
	
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
	err := l.db.QueryRow("SELECT COUNT(*) FROM processed_transfers WHERE txid = ?", txid).Scan(&count)
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
	err := l.db.QueryRow(`
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
	err := l.db.QueryRow(`
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
	rows, err := l.db.Query(`
		SELECT id, block_height, miner_address, worker_id, difficulty, timestamp
		FROM shares 
		WHERE timestamp >= ?
		ORDER BY timestamp DESC`, windowStart)
	
	if err != nil {
		return nil, fmt.Errorf("failed to get shares: %w", err)
	}
	defer rows.Close()

	var shares []Share
	for rows.Next() {
		var share Share
		err := rows.Scan(&share.ID, &share.BlockHeight, &share.MinerAddr, 
			&share.WorkerID, &share.Difficulty, &share.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan share: %w", err)
		}
		shares = append(shares, share)
	}
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
	rows, err := l.db.Query(`
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
	rows, err := l.db.Query(`
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
			SET status = ?, txid = ?, sent_at = ?
			WHERE id = ?`, status, *txid, now, paymentID)
	} else if status == "confirmed" {
		_, err = l.db.Exec(`
			UPDATE payments 
			SET status = ?, confirmed_at = ?
			WHERE id = ?`, status, now, paymentID)
	} else {
		_, err = l.db.Exec(`
			UPDATE payments 
			SET status = ?
			WHERE id = ?`, status, paymentID)
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
	err := l.db.QueryRow(`
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

// GetRecentBlocks retrieves recently found blocks
func (l *LedgerDB) GetRecentBlocks(limit int) ([]BlockFound, error) {
	rows, err := l.db.Query(`
		SELECT height, hash, reward_total, timestamp, found_at, processed_at, status
		FROM blocks_found 
		ORDER BY height DESC 
		LIMIT ?`, limit)
	
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blocks: %w", err)
	}
	defer rows.Close()

	var blocks []BlockFound
	for rows.Next() {
		var block BlockFound
		err := rows.Scan(&block.Height, &block.Hash, &block.RewardTotal,
			&block.Timestamp, &block.FoundAt, &block.ProcessedAt, &block.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to scan block: %w", err)
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// GetTopMiners retrieves top performing miners
func (l *LedgerDB) GetTopMiners(limit int) ([]TopMiner, error) {
	rows, err := l.db.Query(`SELECT * FROM top_miners_24h LIMIT ?`, limit)
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


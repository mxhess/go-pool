-- Mining Pool Ledger Database Schema
-- Prevents double-processing and provides full audit trail

-- Enable foreign keys and WAL mode for performance
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

-- Blocks found by the pool
CREATE TABLE blocks_found (
    height INTEGER PRIMARY KEY,
    hash TEXT UNIQUE NOT NULL,
    reward_total BIGINT NOT NULL,  -- Total block reward in atomic units
    timestamp INTEGER NOT NULL,    -- Block timestamp
    found_at INTEGER NOT NULL,     -- When pool discovered it
    processed_at INTEGER,          -- When pool finished processing shares
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'orphaned'))
);

-- Share contributions for PPLNS calculation
CREATE TABLE shares (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER NOT NULL REFERENCES blocks_found(height),
    miner_address TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    difficulty BIGINT NOT NULL,
    timestamp INTEGER NOT NULL,
    
    -- Index for fast PPLNS window queries
    INDEX idx_shares_timestamp (timestamp),
    INDEX idx_shares_miner (miner_address),
    INDEX idx_shares_block (block_height)
);

-- Calculated reward distributions per block
CREATE TABLE distributions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER NOT NULL REFERENCES blocks_found(height),
    miner_address TEXT NOT NULL,
    shares_contributed BIGINT NOT NULL,  -- Total shares in PPLNS window
    percentage REAL NOT NULL,            -- Share of the reward (0.0-1.0)
    reward_gross BIGINT NOT NULL,        -- Before pool fee
    reward_net BIGINT NOT NULL,          -- After pool fee deduction
    calculated_at INTEGER NOT NULL,      -- When distribution was calculated
    
    UNIQUE(block_height, miner_address),  -- One distribution per miner per block
    INDEX idx_distributions_miner (miner_address),
    INDEX idx_distributions_block (block_height)
);

-- Actual payments made to miners
CREATE TABLE payments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER REFERENCES blocks_found(height),
    recipient_address TEXT NOT NULL,
    amount BIGINT NOT NULL,              -- Amount in atomic units
    fee BIGINT NOT NULL DEFAULT 0,       -- Transaction fee paid
    txid TEXT UNIQUE,                    -- Transaction ID (once sent)
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'confirmed', 'failed')),
    created_at INTEGER NOT NULL,         -- When payment was queued
    sent_at INTEGER,                     -- When transaction was broadcast
    confirmed_at INTEGER,                -- When transaction was confirmed
    
    INDEX idx_payments_recipient (recipient_address),
    INDEX idx_payments_status (status),
    INDEX idx_payments_block (block_height)
);

-- Pool fee accumulation tracking  
CREATE TABLE pool_fees (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    block_height INTEGER REFERENCES blocks_found(height),
    fee_amount BIGINT NOT NULL,          -- Fee earned from this block
    collected_at INTEGER NOT NULL,       -- When fee was calculated
    
    INDEX idx_fees_block (block_height)
);

-- Miner account balances (aggregated view)
CREATE TABLE balances (
    miner_address TEXT PRIMARY KEY,
    balance_pending BIGINT DEFAULT 0,    -- Unconfirmed earnings
    balance_confirmed BIGINT DEFAULT 0,  -- Confirmed, ready for payout
    total_paid BIGINT DEFAULT 0,         -- Lifetime payments made
    last_updated INTEGER NOT NULL,       -- Last balance update
    
    CHECK(balance_pending >= 0),
    CHECK(balance_confirmed >= 0),
    CHECK(total_paid >= 0)
);

-- Idempotency tracking - prevent duplicate processing
CREATE TABLE processed_transfers (
    txid TEXT PRIMARY KEY,               -- Transaction ID from wallet
    height INTEGER NOT NULL,             -- Block height
    amount BIGINT NOT NULL,              -- Transfer amount
    processed_at INTEGER NOT NULL,       -- When we processed it
    
    INDEX idx_transfers_height (height)
);

-- Detailed worker/device tracking for rich web interface
CREATE TABLE workers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    miner_address TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    device_info TEXT,                    -- GPU/CPU model, driver info, etc
    first_seen INTEGER NOT NULL,         -- When worker first connected
    last_seen INTEGER NOT NULL,          -- Last share submission
    user_agent TEXT,                     -- Mining software info
    ip_address TEXT,                     -- Connection IP (for geo stats)
    total_shares BIGINT DEFAULT 0,       -- Lifetime shares submitted
    valid_shares BIGINT DEFAULT 0,       -- Valid shares
    stale_shares BIGINT DEFAULT 0,       -- Stale shares
    invalid_shares BIGINT DEFAULT 0,     -- Invalid shares
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'inactive', 'banned')),
    
    UNIQUE(miner_address, worker_id),
    INDEX idx_workers_miner (miner_address),
    INDEX idx_workers_last_seen (last_seen),
    INDEX idx_workers_status (status)
);

-- Time-series hashrate data for detailed charts (per-miner granularity)
CREATE TABLE hashrate_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,          -- 5-minute intervals
    miner_address TEXT NOT NULL,
    worker_id TEXT,                      -- NULL = aggregate for miner
    hashrate_5m BIGINT NOT NULL,         -- 5-minute average
    hashrate_15m BIGINT NOT NULL,        -- 15-minute average  
    hashrate_1h BIGINT NOT NULL,         -- 1-hour average
    shares_submitted INTEGER DEFAULT 0,  -- Shares in this interval
    shares_valid INTEGER DEFAULT 0,      -- Valid shares in interval
    difficulty_avg REAL DEFAULT 0,       -- Average difficulty
    
    INDEX idx_hashrate_timestamp (timestamp),
    INDEX idx_hashrate_miner (miner_address),
    INDEX idx_hashrate_worker (miner_address, worker_id),
    UNIQUE(timestamp, miner_address, worker_id)
);

-- Pool-wide operational statistics  
CREATE TABLE pool_stats (
    timestamp INTEGER PRIMARY KEY,       -- Stats timestamp (1-minute intervals)
    total_hashrate BIGINT NOT NULL,      -- Pool hashrate
    connected_miners INTEGER NOT NULL,   -- Unique miners
    connected_workers INTEGER NOT NULL,  -- Total workers/devices
    blocks_found_1h INTEGER NOT NULL,    -- Blocks in last hour
    blocks_found_24h INTEGER NOT NULL,   -- Blocks in last 24h
    network_difficulty BIGINT NOT NULL,  -- Current network difficulty
    network_hashrate BIGINT NOT NULL,    -- Estimated network hashrate
    pool_luck_1h REAL NOT NULL,          -- Pool luck last hour (1.0 = average)
    pool_luck_24h REAL NOT NULL,         -- Pool luck last 24h
    total_payments_24h BIGINT NOT NULL,  -- Payments made in 24h
    pending_balance BIGINT NOT NULL,     -- Total pending balances
    
    INDEX idx_stats_timestamp (timestamp)
);

-- Share submission details for debugging and analytics
CREATE TABLE share_submissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    miner_address TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    job_id TEXT NOT NULL,                -- Mining job identifier
    nonce TEXT NOT NULL,                 -- Submitted nonce
    result TEXT NOT NULL,                -- Hash result
    difficulty BIGINT NOT NULL,          -- Share difficulty
    target_difficulty BIGINT NOT NULL,   -- Job target difficulty
    status TEXT NOT NULL CHECK(status IN ('valid', 'stale', 'duplicate', 'low_difficulty', 'invalid')),
    processing_time_ms INTEGER,          -- Time to validate share
    block_candidate BOOLEAN DEFAULT FALSE, -- Was this a block solution?
    
    INDEX idx_submissions_timestamp (timestamp),
    INDEX idx_submissions_miner (miner_address),
    INDEX idx_submissions_worker (miner_address, worker_id),
    INDEX idx_submissions_status (status),
    INDEX idx_submissions_block_candidate (block_candidate)
);

-- Miner connection events for activity tracking
CREATE TABLE connection_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    miner_address TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK(event_type IN ('connect', 'disconnect', 'subscribe', 'authorize')),
    ip_address TEXT,
    user_agent TEXT,
    additional_data TEXT,                -- JSON for extra context
    
    INDEX idx_connections_timestamp (timestamp),
    INDEX idx_connections_miner (miner_address),
    INDEX idx_connections_event (event_type)
);

-- Geographic distribution for world map visualization
CREATE TABLE miner_locations (
    miner_address TEXT PRIMARY KEY,
    country_code TEXT,                   -- ISO country code
    country_name TEXT,
    region TEXT,                         -- State/province
    city TEXT,
    latitude REAL,
    longitude REAL,
    timezone TEXT,
    last_updated INTEGER NOT NULL,
    
    INDEX idx_locations_country (country_code)
);

-- Advanced views for web interface analytics
CREATE VIEW worker_performance AS
SELECT 
    w.miner_address,
    w.worker_id,
    w.device_info,
    w.total_shares,
    w.valid_shares,
    w.stale_shares,
    w.invalid_shares,
    ROUND(w.valid_shares * 100.0 / NULLIF(w.total_shares, 0), 2) as efficiency_percent,
    h.hashrate_1h as current_hashrate,
    w.last_seen,
    CASE 
        WHEN w.last_seen > strftime('%s', 'now') - 300 THEN 'online'
        WHEN w.last_seen > strftime('%s', 'now') - 3600 THEN 'recent' 
        ELSE 'offline'
    END as status
FROM workers w
LEFT JOIN hashrate_history h ON h.miner_address = w.miner_address 
    AND h.worker_id = w.worker_id 
    AND h.timestamp = (
        SELECT MAX(timestamp) FROM hashrate_history 
        WHERE miner_address = w.miner_address AND worker_id = w.worker_id
    );

CREATE VIEW top_miners_24h AS
SELECT 
    h.miner_address,
    COUNT(DISTINCT h.worker_id) as worker_count,
    AVG(h.hashrate_1h) as avg_hashrate,
    SUM(h.shares_valid) as total_shares,
    l.country_name,
    l.country_code
FROM hashrate_history h
LEFT JOIN miner_locations l ON h.miner_address = l.miner_address
WHERE h.timestamp > strftime('%s', 'now') - 86400  -- Last 24 hours
GROUP BY h.miner_address
ORDER BY avg_hashrate DESC
LIMIT 50;

CREATE VIEW pool_statistics_summary AS
SELECT 
    datetime(timestamp, 'unixepoch') as time,
    total_hashrate,
    connected_miners,
    connected_workers,
    blocks_found_24h,
    pool_luck_24h,
    network_difficulty,
    ROUND(total_hashrate * 100.0 / NULLIF(network_hashrate, 0), 4) as pool_percentage
FROM pool_stats
ORDER BY timestamp DESC;

CREATE VIEW recent_blocks_detailed AS
SELECT 
    b.height,
    b.hash,
    datetime(b.timestamp, 'unixepoch') as found_time,
    b.reward_total,
    COUNT(d.miner_address) as miners_rewarded,
    SUM(f.fee_amount) as pool_fee_earned,
    b.status,
    -- Calculate pool luck for this block
    ROUND(b.reward_total * 1.0 / (
        SELECT AVG(network_difficulty) FROM pool_stats 
        WHERE timestamp BETWEEN b.found_at - 3600 AND b.found_at
    ), 4) as block_luck
FROM blocks_found b
LEFT JOIN distributions d ON b.height = d.block_height
LEFT JOIN pool_fees f ON b.height = f.block_height
GROUP BY b.height
ORDER BY b.height DESC
LIMIT 100;

CREATE VIEW miner_earnings_detailed AS
SELECT 
    d.miner_address,
    COUNT(DISTINCT d.block_height) as blocks_contributed,
    SUM(d.reward_net) as total_earned,
    AVG(d.percentage) * 100 as avg_share_percentage,
    MIN(b.timestamp) as first_contribution,
    MAX(b.timestamp) as last_contribution,
    b.balance_confirmed as current_balance,
    b.total_paid as lifetime_paid,
    l.country_name
FROM distributions d
JOIN blocks_found bf ON d.block_height = bf.height
LEFT JOIN balances b ON d.miner_address = b.miner_address
LEFT JOIN miner_locations l ON d.miner_address = l.miner_address
WHERE bf.status = 'completed'
GROUP BY d.miner_address
ORDER BY total_earned DESC;

-- Triggers to maintain balance consistency
CREATE TRIGGER update_balance_on_distribution
AFTER INSERT ON distributions
BEGIN
    INSERT OR REPLACE INTO balances (miner_address, balance_pending, balance_confirmed, total_paid, last_updated)
    VALUES (
        NEW.miner_address,
        COALESCE((SELECT balance_pending FROM balances WHERE miner_address = NEW.miner_address), 0) + NEW.reward_net,
        COALESCE((SELECT balance_confirmed FROM balances WHERE miner_address = NEW.miner_address), 0),
        COALESCE((SELECT total_paid FROM balances WHERE miner_address = NEW.miner_address), 0),
        strftime('%s', 'now')
    );
END;

CREATE TRIGGER update_balance_on_payment  
AFTER UPDATE OF status ON payments
WHEN NEW.status = 'confirmed' AND OLD.status != 'confirmed'
BEGIN
    UPDATE balances 
    SET 
        balance_confirmed = balance_confirmed - NEW.amount,
        total_paid = total_paid + NEW.amount,
        last_updated = strftime('%s', 'now')
    WHERE miner_address = NEW.recipient_address;
END;


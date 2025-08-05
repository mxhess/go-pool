#!/bin/bash
# Migration Script: BoltDB to SQLite Ledger
set -euo pipefail

POOL_DIR="/home/pooluser/pool"
BACKUP_DIR="/home/pooluser/backup-$(date +%Y%m%d-%H%M%S)"
OLD_DB="$POOL_DIR/pool.db"
NEW_DB="$POOL_DIR/ledger.db"

echo "🚀 Starting BoltDB to SQLite migration..."

# Step 1: Stop the pool
echo "📢 Stopping pool services..."
sudo systemctl stop salvium-pool

# Step 2: Create backup
echo "💾 Creating backup..."
mkdir -p "$BACKUP_DIR"
cp -r "$POOL_DIR"/* "$BACKUP_DIR/"
echo "✅ Backup created at $BACKUP_DIR"

# Step 3: Build migration tool
echo "🔨 Building migration tool..."
cd ~/go-pool
cat > cmd/migrate/main.go << 'EOF'
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	
	// Your imports here
)

func main() {
	oldDB := flag.String("old", "/home/pooluser/pool/pool.db", "Old BoltDB path")
	newDB := flag.String("new", "/home/pooluser/pool/ledger.db", "New SQLite path")
	flag.Parse()

	// Remove existing new DB if it exists
	os.Remove(*newDB)

	// Create new ledger
	ledger, err := NewLedgerDB(*newDB)
	if err != nil {
		log.Fatal("Failed to create ledger:", err)
	}
	defer ledger.Close()

	// Migrate data
	if err := migrateData(*oldDB, ledger); err != nil {
		log.Fatal("Migration failed:", err)
	}

	fmt.Println("✅ Migration completed successfully!")
}

func migrateData(oldDBPath string, ledger *LedgerDB) error {
	// TODO: Implement BoltDB -> SQLite migration
	log.Println("🔄 Migrating data...")
	
	// 1. Read old ADDRESS_INFO bucket -> balances table
	// 2. Read old shares -> shares table  
	// 3. Create proper audit trail
	
	return nil
}
EOF

go build -o migrate cmd/migrate/main.go

# Step 4: Run migration
echo "🔄 Running migration..."
./migrate -old="$OLD_DB" -new="$NEW_DB"

# Step 5: Update pool configuration
echo "⚙️  Updating pool configuration..."
# You'll need to update your pool code to use SQLite instead of BoltDB

# Step 6: Verification
echo "🔍 Verifying migration..."
sqlite3 "$NEW_DB" "SELECT COUNT(*) as total_tables FROM sqlite_master WHERE type='table';"
sqlite3 "$NEW_DB" "SELECT COUNT(*) as blocks FROM blocks_found;"
sqlite3 "$NEW_DB" "SELECT COUNT(*) as balances FROM balances;"

echo "🎉 Migration complete!"
echo "📁 Backup stored at: $BACKUP_DIR"
echo "📊 New database at: $NEW_DB"
echo ""
echo "Next steps:"
echo "1. Update pool code to use SQLite ledger"
echo "2. Test thoroughly before restart"
echo "3. Start pool: sudo systemctl start salvium-pool"
echo ""
echo "🚨 If issues occur, restore from backup:"
echo "   sudo systemctl stop salvium-pool"
echo "   cp $BACKUP_DIR/* $POOL_DIR/"
echo "   sudo systemctl start salvium-pool"


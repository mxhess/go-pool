package main

import (
	"flag"
	"fmt"
	"log"
	"go-pool/database"
	bolt "go.etcd.io/bbolt"
)

func main() {
	dbPath := flag.String("db", "pool.db", "Database path")
	confirm := flag.Bool("confirm", false, "Actually clear pending transactions")
	flag.Parse()

	if !*confirm {
		fmt.Println("This will clear ALL pending transactions!")
		fmt.Println("Run with -confirm to actually do it")
		return
	}

	db, err := bolt.Open(*dbPath, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		pendingBuck := tx.Bucket(database.PENDING)
		if pendingBuck == nil {
			return fmt.Errorf("PENDING bucket not found")
		}

		// Create empty pending structure
		pending := database.PendingBals{
			LastHeight:     284800, // Keep current height
			UnconfirmedTxs: []database.UnconfTx{}, // Clear all pending
		}

		fmt.Println("Clearing all pending transactions")
		return pendingBuck.Put([]byte("pending"), pending.Serialize())
	})

	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Pending transactions cleared!")
}

package main

import (
	"flag"
	"fmt"
	"log"
	"time"
	"go-pool/database"
	bolt "go.etcd.io/bbolt"
)

func main() {
	dbPath := flag.String("db", "pool.db", "Database path")
	bucket := flag.String("bucket", "", "Specific bucket to inspect")
	flag.Parse()

	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
	    log.Fatal("Cannot open database - is the pool running? Stop it first:", err)
	}

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.View(func(tx *bolt.Tx) error {
		if *bucket != "" {
			// Show specific bucket
			b := tx.Bucket([]byte(*bucket))
			if b == nil {
				fmt.Printf("Bucket '%s' not found\n", *bucket)
				return nil
			}
			fmt.Printf("=== Bucket: %s ===\n", *bucket)
			return showBucket(b, *bucket)
		}

		// Show all buckets
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			fmt.Printf("\n=== Bucket: %s ===\n", name)
			return showBucket(b, string(name))
		})
	})
}

func showBucket(b *bolt.Bucket, bucketName string) error {
	count := 0
	
	return b.ForEach(func(k, v []byte) error {
		count++
		
		// Try to identify bucket by common key patterns
		if len(k) > 50 && count <= 5 { // Likely ADDRESS_INFO (long address keys)
			fmt.Printf("Address: %s\n", k)
			addrInfo := database.AddrInfo{}
			if err := addrInfo.Deserialize(v); err == nil {
				fmt.Printf("  Balance: %d (%.8f SAL)\n", addrInfo.Balance, float64(addrInfo.Balance)/100000000)
				fmt.Printf("  Paid: %d (%.8f SAL)\n", addrInfo.Paid, float64(addrInfo.Paid)/100000000)
			} else {
				fmt.Printf("  Raw data: %d bytes\n", len(v))
			}
			return nil
		}
		
		if string(k) == "pending" { // PENDING bucket
			fmt.Printf("Key: pending\n")
			pending := database.PendingBals{}
			if err := pending.Deserialize(v); err == nil {
				fmt.Printf("  LastHeight: %d\n", pending.LastHeight)
				fmt.Printf("  UnconfirmedTxs: %d\n", len(pending.UnconfirmedTxs))
				for i, tx := range pending.UnconfirmedTxs {
					fmt.Printf("    Tx %d: Height %d, Balances: %d addresses, BalancesAdded: %t\n", 
						i, tx.UnlockHeight, len(tx.Bals), tx.BalancesAdded)
				}
			}
			return nil
		}
		
		// Generic display for other data
		if count <= 3 {
			fmt.Printf("  Key: %x, Value: %d bytes\n", k, len(v))
		}
		return nil
	})
}


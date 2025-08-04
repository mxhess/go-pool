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
	if bucketName == "ADDRESS_INFO" {
		return b.ForEach(func(k, v []byte) error {
			fmt.Printf("Address: %s\n", k)
			addrInfo := database.AddrInfo{}
			if err := addrInfo.Deserialize(v); err == nil {
				fmt.Printf("  Balance: %d (%.8f SAL)\n", addrInfo.Balance, float64(addrInfo.Balance)/100000000)
				fmt.Printf("  Paid: %d (%.8f SAL)\n", addrInfo.Paid, float64(addrInfo.Paid)/100000000)
			}
			return nil
		})
	}
	
	// Generic bucket display
	count := 0
	return b.ForEach(func(k, v []byte) error {
		count++
		if count <= 10 {
			fmt.Printf("  %x: %d bytes\n", k, len(v))
		}
		return nil
	})
}

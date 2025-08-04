package main

import (
	"fmt"
	"log"
	"go-pool/database"
	bolt "go.etcd.io/bbolt"
)

func main() {
	db, err := bolt.Open("/home/pooluser/pool/pool.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.View(func(tx *bolt.Tx) error {
		// Check ADDRESS_INFO bucket specifically
		infoBuck := tx.Bucket(database.ADDRESS_INFO)
		if infoBuck != nil {
			fmt.Println("=== ADDRESS_INFO Bucket ===")
			infoBuck.ForEach(func(k, v []byte) error {
				fmt.Printf("Address: %s\n", k)
				addrInfo := database.AddrInfo{}
				err := addrInfo.Deserialize(v)
				if err != nil {
					fmt.Printf("  Error deserializing: %v\n", err)
				} else {
					fmt.Printf("  Balance: %d (%.8f SAL)\n", addrInfo.Balance, float64(addrInfo.Balance)/100000000)
					fmt.Printf("  Paid: %d\n", addrInfo.Paid)
				}
				fmt.Println()
				return nil
			})
		}

		// Check other buckets
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			if string(name) != "ADDRESS_INFO" {
				fmt.Printf("\nBucket: %s\n", name)
				count := 0
				b.ForEach(func(k, v []byte) error {
					count++
					if count <= 3 { // Only show first 3 entries
						fmt.Printf("  Key: %x, Value length: %d\n", k, len(v))
					}
					return nil
				})
				if count > 3 {
					fmt.Printf("  ... (%d more entries)\n", count-3)
				}
			}
			return nil
		})
	})
	if err != nil {
		log.Fatal(err)
	}
}


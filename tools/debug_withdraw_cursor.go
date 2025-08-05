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
		buck := tx.Bucket(database.ADDRESS_INFO)
		curs := buck.Cursor()
		
		fmt.Println("=== Cursor iteration (same as withdraw function) ===")
		for key, val := curs.First(); key != nil; key, val = curs.Next() {
			address := string(key)
			fmt.Printf("Address: %s\n", address)
			
			addrInfo := database.AddrInfo{}
			err := addrInfo.Deserialize(val)
			if err != nil {
				fmt.Printf("  Deserialize error: %v\n", err)
			} else {
				fmt.Printf("  Balance: %d (%.8f SAL)\n", addrInfo.Balance, float64(addrInfo.Balance)/100000000)
				fmt.Printf("  Paid: %d\n", addrInfo.Paid)
			}
			fmt.Println()
		}
		return nil
	})
	
	if err != nil {
		log.Fatal(err)
	}
}

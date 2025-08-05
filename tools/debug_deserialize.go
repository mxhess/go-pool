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

	testAddr := "SaLvdV2Bs5vHGpc5G5FQw4MzkrbhZvo6ea2Q6r6bfvtjij8aQa8jPRSGrebWQpJq34eQMCUm44agaTZ9NmoHt3d1JxHXkDYE2pK"

	err = db.View(func(tx *bolt.Tx) error {
		buck := tx.Bucket(database.ADDRESS_INFO)
		val := buck.Get([]byte(testAddr))
		
		fmt.Printf("Raw data length: %d bytes\n", len(val))
		fmt.Printf("Raw data: %x\n", val)
		
		addrInfo := database.AddrInfo{}
		err := addrInfo.Deserialize(val)
		
		fmt.Printf("Deserialize error: %v\n", err)
		fmt.Printf("Balance after deserialize: %d\n", addrInfo.Balance)
		fmt.Printf("Paid after deserialize: %d\n", addrInfo.Paid)
		
		return nil
	})
	
	if err != nil {
		log.Fatal(err)
	}
}

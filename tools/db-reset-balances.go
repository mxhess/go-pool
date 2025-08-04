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
	confirm := flag.Bool("confirm", false, "Actually perform the reset")
	flag.Parse()

	if !*confirm {
		fmt.Println("This will reset ALL address balances to 0!")
		fmt.Println("Run with -confirm to actually do it")
		return
	}

	db, err := bolt.Open(*dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
	    log.Fatal("Cannot open database - is the pool running? Stop it first:", err)
	}

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		buck := tx.Bucket(database.ADDRESS_INFO)
		if buck == nil {
			return fmt.Errorf("ADDRESS_INFO bucket not found")
		}

		return buck.ForEach(func(k, v []byte) error {
			addrInfo := database.AddrInfo{}
			if err := addrInfo.Deserialize(v); err != nil {
				return err
			}

			fmt.Printf("Resetting %s: %.8f SAL -> 0\n", k, float64(addrInfo.Balance)/100000000)
			
			addrInfo.Balance = 0
			// Keep Paid history for record keeping
			
			return buck.Put(k, addrInfo.Serialize())
		})
	})

	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("All balances reset!")
}

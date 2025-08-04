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
	address := flag.String("address", "", "Address to modify")
	balance := flag.Uint64("balance", 0, "New balance in atomic units")
	confirm := flag.Bool("confirm", false, "Actually perform the change")
	flag.Parse()

	if *address == "" {
		log.Fatal("Address required")
	}

	if !*confirm {
		fmt.Printf("Would set %s balance to %d atomic units (%.8f SAL)\n", 
			*address, *balance, float64(*balance)/100000000)
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

		val := buck.Get([]byte(*address))
		addrInfo := database.AddrInfo{}
		
		if val != nil {
			if err := addrInfo.Deserialize(val); err != nil {
				return err
			}
			fmt.Printf("Old balance: %.8f SAL\n", float64(addrInfo.Balance)/100000000)
		}

		addrInfo.Balance = *balance
		fmt.Printf("New balance: %.8f SAL\n", float64(addrInfo.Balance)/100000000)
		
		return buck.Put([]byte(*address), addrInfo.Serialize())
	})

	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Balance updated!")
}

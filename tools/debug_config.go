package main

import (
	"fmt"
	"math"
)

func main() {
	atomic := 8  // From your config.json
	minWithdrawal := 0.1 // From your config.json
	
	fmt.Printf("atomic: %d\n", atomic)
	fmt.Printf("math.Pow10(atomic): %f\n", math.Pow10(atomic))
	fmt.Printf("MinWithdrawal: %f\n", minWithdrawal)
	fmt.Printf("MinWithdrawal in atomic: %f\n", minWithdrawal*math.Pow10(atomic))
	
	balance := uint64(9375928351)
	fmt.Printf("Balance: %d\n", balance)
	fmt.Printf("Balance in SAL: %f\n", float64(balance)/math.Pow10(atomic))
	
	// Test the comparison
	minWithdrawalAtomic := uint64(minWithdrawal*math.Pow10(atomic))
	fmt.Printf("Balance > MinWithdrawal? %t (%d > %d)\n", balance > minWithdrawalAtomic, balance, minWithdrawalAtomic)
}


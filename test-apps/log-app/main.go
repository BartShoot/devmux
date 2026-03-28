package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("VITE v5.0.0 initializing...")
	time.Sleep(3 * time.Second)
	fmt.Println("VITE v5.0.0 ready")
	
	// Keep running
	for {
		time.Sleep(1 * time.Hour)
	}
}

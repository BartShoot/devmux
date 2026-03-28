package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("VITE v5.0.0 initializing...")
	time.Sleep(3 * time.Second)
	fmt.Println("VITE v5.0.0 ready")
	
	// Keep running with periodic output
	for {
		fmt.Printf("[%s] [info] dev-server heartbeat - listening on http://localhost:5173\n", time.Now().Format("15:04:05"))
		time.Sleep(2 * time.Second)
	}
}

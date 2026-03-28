package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/actuator/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] GET /actuator/health - 200 OK\n", time.Now().Format("15:04:05"))
		fmt.Fprintf(w, `{"status":"UP"}`)
	})
	fmt.Println("HTTP server starting on :8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		fmt.Printf("HTTP server error: %v\n", err)
	}
}

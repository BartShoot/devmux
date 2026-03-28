package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/actuator/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"UP"}`)
	})
	fmt.Println("HTTP server starting on :8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		fmt.Printf("HTTP server error: %v\n", err)
	}
}

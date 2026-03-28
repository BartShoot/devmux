package main

import (
	"fmt"
	"net"
)

func main() {
	ln, err := net.Listen("tcp", ":5432")
	if err != nil {
		fmt.Printf("Error starting TCP server: %v\n", err)
		return
	}
	fmt.Println("TCP server listening on :5432")
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("Error accepting connection: %v\n", err)
			continue
		}
		conn.Close()
	}
}

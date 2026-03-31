package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:5432")
	if err != nil {
		fmt.Printf("Error starting TCP server: %v\n", err)
		return
	}
	fmt.Println("TCP server listening on 127.0.0.1:5432")
	for {
		conn, err := ln.Accept()
		if err == nil {
			fmt.Printf("[%s] Incoming connection from %s\n", time.Now().Format("15:04:05"), conn.RemoteAddr())
			conn.Close()
		}
	}
}

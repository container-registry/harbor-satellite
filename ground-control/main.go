package main

import (
	"fmt"
	"log"

	"container-registry.com/harbor-satellite/ground-control/internal/server"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	server := server.NewServer()

	fmt.Printf("Ground Control running on port %s\n", server.Addr)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalf("cannot start server: %s", err)
	}
}

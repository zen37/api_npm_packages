package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/zen37/npm_packages/api"
)

func main() {
	handler := api.New()
	port := os.Getenv("PORT") // Use environment variable for the port
	if port == "" {
		port = "3003" // Default to port ... if not set
	}
	fmt.Printf("Server running on http://0.0.0.0:%s/\n", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

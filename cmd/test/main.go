package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/openmined/syftbox/internal/syftsdk"
)

func main() {
	sdk, err := syftsdk.New(&syftsdk.SyftSDKConfig{
		BaseURL: "http://localhost:8080",
		Email:   "test@test.com",
	})
	if err != nil {
		log.Fatalf("failed to create sdk: %v", err)
	}

	view, err := sdk.Datasite.GetView(context.Background(), &syftsdk.DatasiteViewParams{})
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("view: %v\n", view)
}

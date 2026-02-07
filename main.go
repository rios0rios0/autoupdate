package main

import (
	"os"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		logger.Errorf("Error: %v", err)
		os.Exit(1)
	}
}

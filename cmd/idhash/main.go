package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

var (
	flagMode    = flag.String("mode", "", "encrypt | decrypt")
	envFileFlag = flag.String("env", "", "Path to .env file, 'stdin' or empty")
	idFlag      = flag.String("id", "", "Actual ID")
)

func main() {
	flag.Parse()

	env, err := common.NewEnvMap(*envFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
	cfg := config.NewEnvConfig(env.Get)

	hasher := common.NewIDHasher(cfg.Get(common.IDHasherSaltKey))

	switch *flagMode {
	case "encrypt":
		id, err := strconv.Atoi(*idFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deserializing ID: %v\n", err)
			os.Exit(1)
		}

		value := hasher.Encrypt(id)
		fmt.Print(value)
	case "decrypt":
		id, err := hasher.Decrypt(*idFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error decrypting ID: %v\n", err)
			os.Exit(1)
		}

		fmt.Print(id)
	}
}

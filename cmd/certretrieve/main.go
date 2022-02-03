package main

import (
	"flag"
	"log"
	"os"

	certretrival "github.com/edgefarm/vault-integration/pkg/certretrieval"
)

func setFallbackByEnd(target *string, envName string) {
	if *target == "" {
		*target = os.Getenv(envName)
	}
}

func main() {
	println("Certretrieval for edgefarm")

	config := certretrival.Config{}
	flags := flag.NewFlagSet("certretrieval", flag.ExitOnError)
	flags.StringVar(&config.Tokenfile, "tokenfile", "", "The vault tokenfile (env: VAULT_TOKEN)")
	flags.StringVar(&config.Name, "name", "", "(env: COMMON_NAME)")
	flags.StringVar(&config.OutCAfile, "ca", "", "(env: CA_FILE)")
	flags.StringVar(&config.OutCertfile, "cert", "", "(env: CERT_FILE)")
	flags.StringVar(&config.OutKeyfile, "key", "", "(env: KEY_FILE)")
	flags.StringVar(&config.Role, "role", "", "(env: ROLE)")
	flags.StringVar(&config.ServerCA, "serverca", "", "(env: VAULT_CACERT)")
	flags.StringVar(&config.Vault, "vault", "", "(env: VAULT_ADDR)")

	setFallbackByEnd(&config.Tokenfile, "VAULT_TOKEN")
	setFallbackByEnd(&config.Name, "COMMON_NAME")
	setFallbackByEnd(&config.OutCAfile, "CA_FILE")
	setFallbackByEnd(&config.OutCertfile, "CERT_FILE")
	setFallbackByEnd(&config.OutKeyfile, "KEY_FILE")
	setFallbackByEnd(&config.Role, "ROLE")
	setFallbackByEnd(&config.ServerCA, "VAULT_CACERT")
	setFallbackByEnd(&config.Vault, "VAULT_ADDR")

	cr, err := certretrival.New(config)
	if err != nil {
		log.Fatalf("Failed to create cert retrieval: %v", err)
	}

	if err := cr.Retrieve(); err != nil {
		log.Fatalf("Failed to retrieve cert: %v", err)
	}
}
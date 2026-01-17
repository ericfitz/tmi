// +build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/godror/godror"
)

func main() {
	// Build DSN from environment
	user := "ADMIN"
	password := os.Getenv("ORACLE_PASSWORD")
	if password == "" {
		log.Fatal("ORACLE_PASSWORD environment variable not set")
	}
	walletDir := os.Getenv("TNS_ADMIN")
	if walletDir == "" {
		walletDir = "/Users/efitz/Projects/tmi/wallet"
	}
	connectString := "tmiadb_medium"

	dsn := fmt.Sprintf(`user="%s" password="%s" connectString="%s" configDir="%s"`,
		user, password, connectString, walletDir)

	db, err := sql.Open("godror", dsn)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping: %v", err)
	}
	fmt.Println("Connected to Oracle ADB successfully")

	// Get list of tables to drop (in dependency order - children first)
	tables := []string{
		"audit_entries",
		"webhook_deliveries",
		"webhook_subscriptions",
		"rate_limits",
		"version_history",
		"collaborators",
		"collaboration_sessions",
		"repository_members",
		"repositories",
		"notes",
		"documents",
		"threat_model_accesses",
		"threats",
		"assets",
		"diagrams",
		"threat_models",
		"addon_relationships",
		"addons",
		"group_memberships",
		"groups",
		"client_credentials",
		"refresh_tokens",
		"users",
	}

	for _, table := range tables {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE %s CASCADE CONSTRAINTS", table))
		if err != nil {
			fmt.Printf("Could not drop %s (may not exist): %v\n", table, err)
		} else {
			fmt.Printf("Dropped table: %s\n", table)
		}
	}

	fmt.Println("Done!")
}

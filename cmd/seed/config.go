package main

import (
	"fmt"

	"github.com/ericfitz/tmi/test/testdb"
)

func runConfigSeed(_ *testdb.TestDB, _, _ string, _, _ bool) error {
	return fmt.Errorf("config seed mode not yet implemented")
}

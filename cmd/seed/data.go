package main

import (
	"fmt"

	"github.com/ericfitz/tmi/test/testdb"
)

func runDataSeed(_ *testdb.TestDB, _, _, _, _ string, _ bool) error {
	return fmt.Errorf("data seed mode not yet implemented")
}

package api

import (
	"os"
	"testing"

	"leadmgr/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }

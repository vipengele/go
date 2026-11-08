package service

import (
	"flag"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Bool("update", false, "rewrite the golden files under testdata")
	os.Exit(m.Run())
}

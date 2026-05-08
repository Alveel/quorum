package server

import (
	"os"
	"testing"

	"github.com/alveel/quorum/internal/locale"
)

func TestMain(m *testing.M) {
	locale.Init()
	os.Exit(m.Run())
}

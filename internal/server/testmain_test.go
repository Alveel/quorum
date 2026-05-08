package server

import (
	"os"
	"testing"

	"github.com/alveel/vacation-coverage/internal/locale"
)

func TestMain(m *testing.M) {
	locale.Init()
	os.Exit(m.Run())
}

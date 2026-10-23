package container

import (
	"bytes"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResearchDatabaseLoggerDoesNotPrintBoundSecrets(t *testing.T) {
	var output bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: newDatabaseLogger(&output)})
	if err != nil {
		t.Fatal(err)
	}
	secret := "SYNTHETIC_AUTH_TOKEN_MUST_NOT_APPEAR"
	// An intentional SQL error exercises GORM's real error logging path.
	if err = db.Exec("INSERT INTO nonexistent_auth_tokens (token) VALUES (?)", secret).Error; err == nil {
		t.Fatal("expected missing table")
	}
	if strings.Contains(output.String(), secret) || !strings.Contains(output.String(), "VALUES (?)") {
		t.Fatalf("SQL logging must retain placeholders only: %s", output.String())
	}
}

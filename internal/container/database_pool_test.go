package container

import "testing"

func TestDatabasePoolLimitsFromEnv(t *testing.T) {
	tests := []struct {
		name, driver, open, idle string
		want                     databasePoolLimits
	}{
		{"postgres defaults unchanged", "postgres", "", "", databasePoolLimits{0, 10}},
		{"mysql defaults unchanged", "mysql", "", "", databasePoolLimits{0, 10}},
		{"explicit local limits", "postgres", "20", "5", databasePoolLimits{20, 5}},
		{"trim whitespace", "postgres", " 20 ", " 5\n", databasePoolLimits{20, 5}},
		{"idle bounded by open", "postgres", "5", "20", databasePoolLimits{5, 5}},
		{"default idle bounded by open", "postgres", "3", "", databasePoolLimits{3, 3}},
		{"zero open preserves unlimited", "postgres", "0", "8", databasePoolLimits{0, 8}},
		{"zero idle disables idle cache", "postgres", "20", "0", databasePoolLimits{20, 0}},
		{"negative values use defaults", "postgres", "-5", "-2", databasePoolLimits{0, 10}},
		{"invalid values use defaults", "postgres", "many", "1.5", databasePoolLimits{0, 10}},
		{"huge values are bounded", "postgres", "999999", "999999", databasePoolLimits{1000, 1000}},
		{"integer overflow is bounded", "postgres", "999999999999999999999999", "3", databasePoolLimits{1000, 3}},
		{"sqlite ignores deployment limits", "sqlite", "20", "5", databasePoolLimits{1, 1}},
		{"sqlite remains one without limits", "sqlite", "0", "0", databasePoolLimits{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "WEKNORA_DB_MAX_OPEN_CONNS":
					return tt.open
				case "WEKNORA_DB_MAX_IDLE_CONNS":
					return tt.idle
				default:
					t.Fatalf("unexpected environment key: %s", key)
					return ""
				}
			}
			if got := databasePoolLimitsFromEnv(tt.driver, getenv); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

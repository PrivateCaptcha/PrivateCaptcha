package db

import (
	"strings"
	"testing"
)

func TestClickHouseFormSubmitLogMigrations(t *testing.T) {
	tests := []struct {
		path     string
		contains []string
	}{
		{
			path: "migrations/clickhouse/000011_create_form_submit_logs.up.sql",
			contains: []string{
				"CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs",
				"user_id UInt32",
				"org_id UInt32",
				"property_id UInt32",
				"form_id UInt32",
				"status UInt8",
				"timestamp DateTime",
				"ENGINE = Null",
			},
		},
		{
			path: "migrations/clickhouse/000012_aggregate_form_submit_logs_1h.up.sql",
			contains: []string{
				"CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs_1h",
				"success_count UInt32",
				"failure_count UInt32",
				"ORDER BY (user_id, org_id, property_id, form_id, timestamp)",
				"CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.form_submit_logs_1h_mv",
				"countIf(status = 0) AS success_count",
				"countIf(status != 0) AS failure_count",
				"FROM privatecaptcha.form_submit_logs",
			},
		},
		{
			path: "migrations/clickhouse/000013_aggregate_form_submit_logs_1d.up.sql",
			contains: []string{
				"CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs_1d",
				"success_count UInt64",
				"failure_count UInt64",
				"ORDER BY (user_id, org_id, property_id, form_id, timestamp)",
				"CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.form_submit_logs_1d_mv",
				"sum(success_count) AS success_count",
				"sum(failure_count) AS failure_count",
				"FROM privatecaptcha.form_submit_logs_1h",
			},
		},
		{
			path: "migrations/clickhouse/000014_aggregate_form_submit_logs_1mo.up.sql",
			contains: []string{
				"CREATE TABLE IF NOT EXISTS privatecaptcha.form_submit_logs_1mo",
				"form_id UInt32",
				"success_count UInt64",
				"failure_count UInt64",
				"ORDER BY (user_id, org_id, property_id, form_id, timestamp)",
				"CREATE MATERIALIZED VIEW IF NOT EXISTS privatecaptcha.form_submit_logs_1mo_mv",
				"toStartOfMonth(timestamp) AS timestamp",
				"FROM privatecaptcha.form_submit_logs_1d",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			contents, err := clickhouseMigrationsFS.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("failed to read migration: %v", err)
			}

			migration := string(contents)
			for _, expected := range tt.contains {
				if !strings.Contains(migration, expected) {
					t.Fatalf("migration %s missing %q", tt.path, expected)
				}
			}
		})
	}

	downMigrations := []string{
		"migrations/clickhouse/000011_create_form_submit_logs.down.sql",
		"migrations/clickhouse/000012_aggregate_form_submit_logs_1h.down.sql",
		"migrations/clickhouse/000013_aggregate_form_submit_logs_1d.down.sql",
		"migrations/clickhouse/000014_aggregate_form_submit_logs_1mo.down.sql",
	}
	for _, path := range downMigrations {
		t.Run(path, func(t *testing.T) {
			contents, err := clickhouseMigrationsFS.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read migration: %v", err)
			}
			if !strings.Contains(string(contents), "DROP") {
				t.Fatalf("down migration %s does not drop objects", path)
			}
		})
	}
}

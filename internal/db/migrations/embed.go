// SPDX-License-Identifier: AGPL-3.0-or-later

// Package migrations exposes the SQL migration files as an embedded
// filesystem. The controller (and the integration-test harness) consume
// this FS via goose to bring a database up to schema.
package migrations

import "embed"

// FS exposes every .sql file in this package as a read-only filesystem
// for goose to consume at runtime.
//
//go:embed *.sql
var FS embed.FS

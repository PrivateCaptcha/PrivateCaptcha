# Repository Changelog

This document summarizes changes on `origin/main` by git tag. Each section covers the commit range for that release and focuses on operator-, developer-, and user-visible changes rather than commit-by-commit history.

## Sections

- [Unreleased](#unreleased)
- [v0.0.1 — 2025-07-09](#v001-2025-07-09)
- [v0.0.2 — 2025-07-14](#v002-2025-07-14)
- [v0.0.3 — 2025-07-14](#v003-2025-07-14)
- [v0.0.4 — 2025-07-18](#v004-2025-07-18)
- [v0.0.5 — 2025-07-31](#v005-2025-07-31)
- [v0.0.6 — 2025-08-18](#v006-2025-08-18)
- [v0.0.7 — 2025-08-20](#v007-2025-08-20)
- [v0.0.8 — 2025-08-25](#v008-2025-08-25)
- [v0.0.9 — 2025-09-03](#v009-2025-09-03)
- [v0.0.10 — 2025-09-03](#v0010-2025-09-03)
- [v0.0.11 — 2025-09-12](#v0011-2025-09-12)
- [v0.0.12 — 2025-09-15](#v0012-2025-09-15)
- [v0.0.13 — 2025-09-24](#v0013-2025-09-24)
- [v0.0.14 — 2025-09-30](#v0014-2025-09-30)
- [v0.0.15 — 2025-09-30](#v0015-2025-09-30)
- [v0.0.16 — 2025-10-04](#v0016-2025-10-04)
- [v0.0.17 — 2025-10-04](#v0017-2025-10-04)
- [v0.0.18 — 2025-10-06](#v0018-2025-10-06)
- [v0.0.19 — 2025-10-08](#v0019-2025-10-08)
- [v0.0.20 — 2025-10-11](#v0020-2025-10-11)
- [v0.0.21 — 2025-10-21](#v0021-2025-10-21)
- [v0.0.22 — 2025-11-13](#v0022-2025-11-13)
- [v0.0.23 — 2025-11-20](#v0023-2025-11-20)
- [v0.0.24 — 2025-11-29](#v0024-2025-11-29)
- [v0.0.25 — 2025-12-03](#v0025-2025-12-03)
- [v0.0.26 — 2025-12-19](#v0026-2025-12-19)
- [v0.0.27 — 2025-12-23](#v0027-2025-12-23)
- [v1.28.0 — 2026-01-27](#v1280-2026-01-27)
- [v1.28.1 — 2026-01-28](#v1281-2026-01-28)
- [v1.28.2 — 2026-01-28](#v1282-2026-01-28)
- [v1.29.0 — 2026-02-09](#v1290-2026-02-09)
- [v1.30.0 — 2026-03-02](#v1300-2026-03-02)
- [v1.30.1 — 2026-03-03](#v1301-2026-03-03)
- [v1.30.2 — 2026-03-04](#v1302-2026-03-04)
- [v1.30.3 — 2026-03-10](#v1303-2026-03-10)
- [v1.31.0 — 2026-03-17](#v1310-2026-03-17)
- [v1.31.1 — 2026-03-19](#v1311-2026-03-19)

## Unreleased

_Range: `v1.31.1..origin/main` · 0 commits_

- No unreleased changes are present on `origin/main` after `v1.31.1`.

## v0.0.1 — 2025-07-09

_Range: `v0.0.1 (initial history through this tag)`_

- Initial public release of the PrivateCaptcha stack, including the Go server, API, portal, widget, Docker setup, and CI/build workflows.
- Established core security and operations foundations such as security headers, request tracing, rate limiting, caching improvements, monitoring, and more resilient verification behavior.
- Added the first OpenAPI definition, widget package publishing flow, and early enterprise licensing and activation support.

## v0.0.2 — 2025-07-14

_Range: `v0.0.1..v0.0.2`_

- Small maintenance release with dependency refreshes and a fix for the enterprise activation URL format.

## v0.0.3 — 2025-07-14

_Range: `v0.0.2..v0.0.3`_

- Split the PrivateCaptcha verify API from the reCAPTCHA-compatible `/siteverify` flow.
- Improved verification responses and fixed fast-path handling for test puzzles.

## v0.0.4 — 2025-07-18

_Range: `v0.0.3..v0.0.4`_

- Made `/siteverify` fully reCAPTCHA-compatible, including successful handling for echo puzzles.
- Simplified the PrivateCaptcha-specific verify response format.

## v0.0.5 — 2025-07-31

_Range: `v0.0.4..v0.0.5`_

- Improved runtime robustness with explicit ClickHouse read timeouts, safer context handling, and live log-level changes.
- Fixed API key limit updates and several cache/context edge cases in backend code.
- Reworked integrations content into data-driven portal pages and refreshed supporting dependencies.

## v0.0.6 — 2025-08-18

_Range: `v0.0.5..v0.0.6`_

- Added a full notifications foundation, including persistent notifications, expiring API key alerts, and richer welcome emails.
- Expanded maintenance jobs for offboarding and trial expiry, with safer execution, lock checks, circuit breaking, and argument support.
- Added a Google reCAPTCHA-compatible site script mode, jittered `Retry-After` responses, and standardized configuration under `PC_` environment variables.

## v0.0.7 — 2025-08-20

_Range: `v0.0.6..v0.0.7`_

- Cached stub puzzles and expanded the integrations page with PHP and Ruby guidance.
- Otherwise a small maintenance release.

## v0.0.8 — 2025-08-25

_Range: `v0.0.7..v0.0.8`_

- Switched puzzle identifiers to XID and advanced the move from `allow_replay` to `max_replay_count`-based replay control.
- Improved puzzle cache and leaky-bucket performance, with better profiling and diagnostics around cache and property failures.
- Hardened the product by requiring property domains in the portal and protecting maintenance-job endpoints.

## v0.0.9 — 2025-09-03

_Range: `v0.0.8..v0.0.9`_

- Replaced widget JavaScript callbacks with DOM events for integrations.
- Refactored puzzle issue/verify handling and corrected puzzle issue timestamps.

## v0.0.10 — 2025-09-03

_Range: `v0.0.9..v0.0.10`_

- Mostly an observability and bug-fix release, correcting healthcheck job exclusivity and improving puzzle decode/debug logging.

## v0.0.11 — 2025-09-12

_Range: `v0.0.10..v0.0.11`_

- Improved structured logging with service tags and cleaner log levels.
- Fixed organization invite and member validation so organization users resolve more reliably.

## v0.0.12 — 2025-09-15

_Range: `v0.0.11..v0.0.12`_

- Refactored session handling and shortened session flush timing so persisted session state updates more quickly.
- Added extra ID-safety checks and general session cleanup around session persistence.

## v0.0.13 — 2025-09-24

_Range: `v0.0.12..v0.0.13`_

- Improved integration UX with visible sitekeys, better localhost layout, and WordPress integration support in the portal.
- Added asset ETags and stylesheet cache busting tied to the current Git commit.
- Restored widget auto-start as the default mode.

## v0.0.14 — 2025-09-30

_Range: `v0.0.13..v0.0.14`_

- Added configurable widget colors and relaxed some profile and onboarding validation.
- Fixed cache reads inside transactions and corrected internal subscription detection.

## v0.0.15 — 2025-09-30

_Range: `v0.0.14..v0.0.15`_

- Ensured account creation returns the refreshed user record.
- Otherwise a tiny maintenance release.

## v0.0.16 — 2025-10-04

_Range: `v0.0.15..v0.0.16`_

- Hardened validation around organization names, signatures, and license activation parsing.
- Moved the XSRF key into explicit configuration.

## v0.0.17 — 2025-10-04

_Range: `v0.0.16..v0.0.17`_

- Added stronger server-side validation for user, property, and API key names.
- Included additional bounds and type-safety checks for security hardening.

## v0.0.18 — 2025-10-06

_Range: `v0.0.17..v0.0.18`_

- Fixed notification cleanup context handling and corrected truncated API key values in notifications.
- Added container image scanning to CI.
- Overall, a small bug-fix and security-maintenance release.

## v0.0.19 — 2025-10-08

_Range: `v0.0.18..v0.0.19`_

- Added aggregated challenge statistics and easier sitekey visibility in the dashboard.
- Introduced API key rotation.
- Fixed missing portal login verification metrics.

## v0.0.20 — 2025-10-11

_Range: `v0.0.19..v0.0.20`_

- Added organization invitation emails and enforced organization ownership-count limits.
- Fixed widget visibility and styling regressions and expanded widget event coverage.
- Polished several portal layouts and settings screens.

## v0.0.21 — 2025-10-21

_Range: `v0.0.20..v0.0.21`_

- Added runtime widget restyling via `data-styles` and automatic language mode.
- Tightened catch-all path rate limiting and only records solutions after real user interaction with the widget.
- Added Go's built-in cross-origin protection middleware to the portal.

## v0.0.22 — 2025-11-13

_Range: `v0.0.21..v0.0.22`_

- Added retrying chart fetches, resend timers, and better metrics for portal HTTP errors before redirects.
- Introduced property move support and hid internal identifiers behind hashed portal URLs.
- Expanded integrations and self-hosted license activation flexibility, with additional security hardening.

## v0.0.23 — 2025-11-20

_Range: `v0.0.22..v0.0.23`_

- Unified login and 2FA into a single flow, with clearer 2FA emails and stale-session backfill support.
- Let organization members use the verify API for shared properties.
- Reworked user limiting to separate API and property checks, with better rechecks, backpressure, and cache tuning.

## v0.0.24 — 2025-11-29

_Range: `v0.0.23..v0.0.24`_

- Added audit logs with portal UI, retention controls, HTML-capable messages, and subscription-specific audit events.
- Expanded usage views with per-property and per-organization usage, longer session TTL, and better sitekey and IP handling.
- Fixed popup auto-start behavior and duplicate trigger execution.

## v0.0.25 — 2025-12-03

_Range: `v0.0.24..v0.0.25`_

- Tightened `/verify` by checking the expected sitekey and returning friendlier verification error bodies.
- Updated the OpenAPI spec to match verification behavior.
- Otherwise a small bug-fix release.

## v0.0.26 — 2025-12-19

_Range: `v0.0.25..v0.0.26`_

- Added the first substantial organizations and properties API: list/get/create/update/delete, bulk property creation, pagination, and OpenAPI coverage.
- Introduced API key scopes and the related database and UI groundwork.
- Greatly improved chart caching at both CDN and server layers while avoiding stale user-specific caching.
- Strengthened auth and operations with better email verification, stronger 2FA randomness, logout cache cleanup, and safer job locking.

## v0.0.27 — 2025-12-23

_Range: `v0.0.26..v0.0.27`_

- Expanded API key permissions with read-only and organization scopes, plus richer audit metadata for API key events.
- Added stricter JSON request validation.
- Otherwise mostly maintenance and publishing updates.

## v1.28.0 — 2026-01-27

_Range: `v0.0.27..v1.28.0`_

- Added `migrate` and `serve` modes, a lighter test path without ClickHouse, and stronger init, CI, and coverage workflows for developers and operators.
- Tightened auth and organization management with 2FA expiration handling, organization transfers, invites for users without accounts, and org-member property creation via API.
- Improved widget robustness and public API coverage, including safer custom-element handling and reset behavior.
- Added org-level usage stats, API key last-used tracking, Java integration support, and faster JSON handling for verify traffic.

## v1.28.1 — 2026-01-28

_Range: `v1.28.0..v1.28.1`_

- Maintenance-only release focused on CI and Docker workflow updates.

## v1.28.2 — 2026-01-28

_Range: `v1.28.1..v1.28.2`_

- Fixed auto-mode database lifecycle so migrations no longer close live connections.
- Expanded easyjson coverage for API types and improved load testing for test puzzles.
- Hardened local Docker setup with read-only containers and public-key checksum visibility.

## v1.29.0 — 2026-02-09

_Range: `v1.28.2..v1.29.0`_

- Added configurable fetch timeouts in the widget to prevent hung network calls.
- Added enable/disable controls for properties and users, with API and portal enforcement.
- Improved operational safety with semaphore-based maintenance-job concurrency, readiness checks that respect maintenance mode, and a circuit breaker for invalid sitekeys.
- Extended licensing and audit visibility with community-edition license checks, unlicensed UI state, masked IPs in audit logs, and a 2FA grace period.

## v1.30.0 — 2026-03-02

_Range: `v1.29.0..v1.30.0`_

- Added tooling to inspect error logs and fetch local 2FA codes from logs.
- Improved deployment behavior with better single-domain support, configurable handler timeouts, async ClickHouse inserts, and panic metrics.
- Fixed organization invite re-access behavior and added portal tab navigation by URL.
- Reworked difficulty computation across client and server, added database safeguards, and adjusted script cache-control behavior.

## v1.30.1 — 2026-03-03

_Range: `v1.30.0..v1.30.1`_

- Made solver cancellation context-aware and improved solver performance.
- Fixed ClickHouse failure rollback handling and smarter cache refresh and bulk rereads.
- Separated stub and non-stub solution counts and shortened caching for test-property API responses.

## v1.30.2 — 2026-03-04

_Range: `v1.30.1..v1.30.2`_

- Maintenance-only hotfix correcting unconditional ClickHouse rollback behavior.

## v1.30.3 — 2026-03-10

_Range: `v1.30.2..v1.30.3`_

- Improved observability with Postgres connection metrics and safer SQL trace logging.
- Fixed canceled-request handling in API and portal responses.
- Prevented chart UI races by disabling period buttons while data is loading.
- Also included metrics-handler cleanup and materially relevant runtime and dependency refreshes.

## v1.31.0 — 2026-03-17

_Range: `v1.30.3..v1.31.0`_

- Introduced a full difficulty-rules system with rule matching, database support, portal UI, and reporting integration.
- Added an unconditional `always` rule condition and increased cache sizing to support rule evaluation.
- Improved notifications with configurable `From`/`Reply-To` overrides, better name parsing, template ID visibility in `viewemails`, and graceful deduplication on conflicts.
- Hardened security and concurrency with IDOR fixes for difficulty-rule mutations, leaky-bucket race fixes, reduced 2FA logging exposure, and related cleanup.

## v1.31.1 — 2026-03-19

_Range: `v1.31.0..v1.31.1`_

- Reorganized the property integrations page and its backing data into clearer sections, making integration guidance easier to scan and navigate.

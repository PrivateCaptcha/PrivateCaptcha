# Repository Changelog

This document summarizes every commit reachable from `origin/main`, grouped by git tag in chronological order.
Each section uses the exact tag range that produced the release, and each bullet preserves the original commit subject together with its date and abbreviated SHA for traceability.

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

## Unreleased

_Range: `v1.31.0..origin/main` · 5 commits_

- 2026-03-17 — `0d09403` — chore(deps): update clickhouse/clickhouse-server docker tag to v26.2.3
- 2026-03-18 — `8737873` — Redirect new registrations to create property page (#398)
- 2026-03-18 — `9ccc7f0` — Fix back URLs
- 2026-03-18 — `412baa0` — Make user org public
- 2026-03-19 — `6833c0c` — Split integrations sections

## v0.0.1 — 2025-07-09

_Range: `v0.0.1 (initial history through this tag)` · 129 commits_

- 2025-05-31 — `4951055` — Initial commit
- 2025-06-01 — `354f854` — Split health metrics into two
- 2025-06-01 — `2eb988d` — Rename metrics
- 2025-06-08 — `2b36f8d` — Improve monitoring
- 2025-06-08 — `1ade63b` — Add basic security headers. closes PrivateCaptcha/issues#148
- 2025-06-08 — `6d745c2` — Add "security" headers also to static assets responses
- 2025-06-11 — `abb6736` — Export VerifyResponse types
- 2025-06-12 — `40368ad` — Allow for variable periodic job intervals
- 2025-06-14 — `a77549d` — Update README.md
- 2025-06-16 — `3041774` — Add nofollow rel attribute to widget link
- 2025-06-16 — `899763c` — Update README.md
- 2025-06-16 — `6ec8779` — Add initial OpenAPI definition
- 2025-06-16 — `6ab977d` — Fix prettier linting for OpenAPI file
- 2025-06-16 — `e99c444` — Change Unauthorized to Forbidden in API key auth
- 2025-06-16 — `4105052` — Update README.md
- 2025-06-16 — `fd0d001` — Fix tests
- 2025-06-17 — `24006e1` — Update OpenAPI spec
- 2025-06-17 — `8bbea21` — Fix extra transaction rollback error log
- 2025-06-17 — `90a0652` — Add alternatives to README
- 2025-06-17 — `2f9b7be` — Fix typo
- 2025-06-17 — `b8fd2d6` — Add renovate to CLA allow list
- 2025-06-17 — `3cd2a5b` — Add renovate config
- 2025-06-17 — `c15e1e3` — Update cla.yml
- 2025-06-17 — `9bbc23e` — Update cla.yml
- 2025-06-17 — `e67a96c` — Update cla.yml
- 2025-06-17 — `c751cf9` — Update dependency go to v1.24.4 (#147)
- 2025-06-17 — `9a9ab4f` — Update golang Docker tag to v1.24.4 (#151)
- 2025-06-17 — `f031d0b` — Update clickhouse/clickhouse-server Docker tag to v24.12.6 (#148)
- 2025-06-17 — `ae1345b` — Add session keys count
- 2025-06-17 — `b51767a` — Set request ID header for API responses
- 2025-06-17 — `86a6b9e` — Update ClickHouse image also in tests
- 2025-06-17 — `f2c64f3` — Cosmetic security improvements to reduce spam from SonarCloud
- 2025-06-17 — `312ee05` — Exclude more codepaths from sonar coverage report
- 2025-06-17 — `09e8cb7` — Update module github.com/golang-migrate/migrate/v4 to v4.18.3 (#153)
- 2025-06-17 — `0a469c7` — Update otter to v2
- 2025-06-18 — `fd47424` — Update module github.com/rs/cors to v1.11.1 (#160)
- 2025-06-18 — `e553512` — Update dependency tailwindcss to v3.4.17 (#159)
- 2025-06-18 — `5abcecd` — Update module github.com/prometheus/client_golang to v1.22.0 (#157)
- 2025-06-18 — `fc9aa0a` — Update dependency @tailwindcss/typography to v0.5.16 (#155)
- 2025-06-19 — `1412a6f` — Update module golang.org/x/net to v0.41.0 (#165)
- 2025-06-19 — `ca84b0b` — Update module github.com/maypok86/otter/v2 to v2.0.0 (#163)
- 2025-06-19 — `22b27b5` — Update module github.com/rs/xid to v1.6.0 (#162)
- 2025-06-19 — `671933d` — Update dependency eslint to v9.29.0 (#150)
- 2025-06-19 — `d0c59dc` — Remove extra async from script example
- 2025-06-19 — `3f3390f` — Update actions/setup-go action to v5 (#167)
- 2025-06-19 — `0d2fb2e` — Update actions/setup-node action to v4 (#168)
- 2025-06-21 — `9ec1ef8` — Refactor caching layer
- 2025-06-21 — `bf0598c` — Make siteverify API more resilient for unauthorized access
- 2025-06-21 — `1dc3301` — Reread cached properties from time to time
- 2025-06-21 — `e3d93fa` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.37.1 (#152)
- 2025-06-21 — `2093f92` — Update module github.com/jackc/pgx/v5 to v5.7.5 (#156)
- 2025-06-21 — `f40abbc` — Update dependency @tailwindcss/forms to v0.5.10 (#154)
- 2025-06-21 — `2e1df61` — Update docker/build-push-action action to v6 (#172)
- 2025-06-21 — `d3a0173` — Update actions/checkout action to v4 (#166)
- 2025-06-21 — `b458a16` — Update module github.com/PuerkitoBio/goquery to v1.10.3 (#161)
- 2025-06-21 — `d6399fd` — Update dependency esbuild to v0.25.5 (#158)
- 2025-06-21 — `8a27ea7` — Update docker/login-action action to v3 (#173)
- 2025-06-21 — `254fdf0` — Bump golangci-lint to v8
- 2025-06-21 — `f05e20e` — Update dependency node to v22 (#170)
- 2025-06-21 — `5cb363b` — Passthrough billing plan for onboarding
- 2025-06-21 — `b8929c3` — Fix typo
- 2025-06-21 — `45d9781` — Cosmetic improvements
- 2025-06-23 — `7e45b52` — Add basic package publishing workflow for core js classes in widget
- 2025-06-23 — `51cd51d` — Fix publishing widget js package
- 2025-06-23 — `04409b0` — Use explicit registry
- 2025-06-23 — `6b3d079` — Only publish package for main branch
- 2025-06-23 — `447af98` — Check if widget library publish is needed in CI
- 2025-06-23 — `e7cad88` — Fix package CI
- 2025-06-23 — `e9ac87b` — Use explicit registry
- 2025-06-23 — `39521d3` — Use a separate job to check package version
- 2025-06-23 — `247a5aa` — Add JS callbacks to widget options
- 2025-06-23 — `9c1dc03` — Update prom/prometheus Docker tag to v3 (#175)
- 2025-06-24 — `9f9c12e` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.37.2 (#176)
- 2025-06-24 — `3b6548a` — Add few more useless badges
- 2025-06-24 — `d3e69dc` — Cosmetic improvements
- 2025-06-27 — `40e8936` — Fix code style issues (#177)
- 2025-06-28 — `ceb9ca7` — Update dependency eslint to v9.30.0 (#179)
- 2025-06-28 — `c0c7515` — Update allow list in CLA
- 2025-06-29 — `b7ef406` — Make widget package public explicitly
- 2025-06-29 — `02184e3` — Bump version
- 2025-06-29 — `a8d848c` — Fix usage of widget library as a package
- 2025-06-29 — `0faa8c5` — Bump widget version
- 2025-06-29 — `d6440d7` — Cosmetic improvements
- 2025-06-30 — `21ceaaf` — Update module github.com/maypok86/otter/v2 to v2.1.0 (#180)
- 2025-06-30 — `4d7aa27` — Add renovate-bot to allowlist
- 2025-07-01 — `ff0b863` — Use the same docker container for migrations in tests
- 2025-07-01 — `ee143f3` — Cosmetic improvement
- 2025-07-02 — `3c2edf6` — Add EE license check functionality
- 2025-07-02 — `4ca1ca8` — Cosmetic improvement
- 2025-07-02 — `2bef7fc` — Fix go list usage without tags
- 2025-07-02 — `555b9a8` — Fix golangci-lint run
- 2025-07-02 — `fbf3381` — Improve fetching EE license
- 2025-07-02 — `d00dbe7` — Fix build tags definition for golangci-lint
- 2025-07-02 — `bab2f69` — Quit server gracefully on license error
- 2025-07-03 — `e88ccf5` — Make system notifications unique
- 2025-07-03 — `893e1fd` — Export signed message
- 2025-07-03 — `b9819b2` — Embed all key files
- 2025-07-03 — `a57d9bb` — Fix linter error
- 2025-07-03 — `e75d4d0` — Return explicit error for empty EE keys
- 2025-07-03 — `09c8c13` — Fix typo
- 2025-07-03 — `e630737` — Fix build
- 2025-07-04 — `219f01f` — Update renovate.json
- 2025-07-04 — `c1e55ee` — Cosmetic improvements
- 2025-07-05 — `6b51632` — Remove EE url and email from configuration
- 2025-07-05 — `7f6042f` — Export cache keys
- 2025-07-05 — `fc87e8b` — Mirror cache key extension logic to env vars
- 2025-07-05 — `d8a2459` — Fix linter error
- 2025-07-05 — `28b73b9` — Fix unlikely init race condition
- 2025-07-05 — `89aeb40` — Export more cache functions and types
- 2025-07-05 — `4a2cb85` — Add comparison disclaimer
- 2025-07-06 — `e63c808` — Update module github.com/maypok86/otter/v2 to v2.1.1
- 2025-07-06 — `36c10eb` — Refactor batch-reading from cache and DB
- 2025-07-06 — `7063435` — Merge branch 'renovate/github.com-maypok86-otter-v2-2.x'
- 2025-07-06 — `3466f9a` — Send stack version during activation
- 2025-07-07 — `42f495c` — Use a single rate limiter based on IPs with views
- 2025-07-07 — `468c9c9` — Fix typo
- 2025-07-07 — `ebc2c27` — Cosmetic improvement
- 2025-07-08 — `f3a2417` — Get rid of public ports for development docker-compose files
- 2025-07-08 — `4af7fc8` — Rename X-Request-ID to X-Trace-ID for consistency
- 2025-07-08 — `be96cf3` — Refactor rate limiter out of auth middleware
- 2025-07-08 — `6bb2d74` — Vibe-code activation keys packer for GitHub Action
- 2025-07-08 — `edf4c66` — Add GitHub Action jobs to publish EE docker image
- 2025-07-09 — `5c7c7d9` — Build all targets in CI
- 2025-07-09 — `03635d2` — Reconfigure sonar coverage exceptions
- 2025-07-09 — `04f305e` — Use environment for docker image push
- 2025-07-09 — `ceac6fe` — Fix calculating tests coverage
- 2025-07-09 — `6dca4fb` — Do not merge coverage report for SonarCloud
- 2025-07-09 — `decbbc8` — Fix typo
- 2025-07-09 — `3dc55c4` — Fix typo in go test command

## v0.0.2 — 2025-07-14

_Range: `v0.0.1..v0.0.2` · 8 commits_

- 2025-07-10 — `395f1ea` — Update dependency esbuild to v0.25.6
- 2025-07-11 — `7c2233d` — Update dependency go to v1.24.5
- 2025-07-12 — `06b8d11` — Update dependency eslint to v9.31.0 (#181)
- 2025-07-13 — `82b8fd6` — Update module github.com/maypok86/otter/v2 to v2.2.0 (#182)
- 2025-07-14 — `a79f15d` — Update golang Docker tag to v1.24.5
- 2025-07-14 — `bb4b9a1` — Remove trailing slash in activation url
- 2025-07-14 — `843844d` — Update module golang.org/x/crypto to v0.40.0 (#183)
- 2025-07-14 — `87d3d09` — Merge remote-tracking branch 'origin/renovate/golang-1.x'

## v0.0.3 — 2025-07-14

_Range: `v0.0.2..v0.0.3` · 7 commits_

- 2025-07-14 — `6f1fa53` — Fix fast path checking for test puzzle
- 2025-07-14 — `ff75226` — Rename test
- 2025-07-14 — `9b34577` — Fix running unit tests
- 2025-07-14 — `4d98f0d` — Split reCAPTCHA compatibility and our own verify endpoint
- 2025-07-14 — `3114d4f` — Fix typo
- 2025-07-14 — `9331704` — Improve verification response
- 2025-07-14 — `c3da4ed` — Fix linter errors

## v0.0.4 — 2025-07-18

_Range: `v0.0.3..v0.0.4` · 8 commits_

- 2025-07-15 — `7d5c0a5` — Rearrange private verify API response fields
- 2025-07-15 — `26d3c2b` — Vibe-code few TS annotations for widget code
- 2025-07-15 — `fa753f2` — Update module golang.org/x/net to v0.42.0 (#184)
- 2025-07-15 — `4f244cf` — Add explicit registry urls
- 2025-07-15 — `0f7f3fb` — Remove string error field from /verify response
- 2025-07-15 — `13b7789` — Fix typo
- 2025-07-18 — `5fd0061` — Return success for echo puzzles
- 2025-07-18 — `182c897` — Make /siteverify fully reCAPTCHA-compatible

## v0.0.5 — 2025-07-31

_Range: `v0.0.4..v0.0.5` · 20 commits_

- 2025-07-18 — `be0dc36` — Update README.md
- 2025-07-19 — `ac9b4ed` — Foolproof usage of internal trial
- 2025-07-19 — `f65d741` — Fix typo
- 2025-07-20 — `daa70d9` — Fix updating non-cached API key limits
- 2025-07-20 — `33c5964` — Cosmetic improvement
- 2025-07-21 — `4eabbb7` — Update dependency esbuild to v0.25.8
- 2025-07-23 — `f6dc6cb` — Update module github.com/maypok86/otter/v2 to v2.2.1
- 2025-07-24 — `c8b1c77` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.39.0 (#185)
- 2025-07-24 — `ec10471` — Set explicit read timeout for clickhouse
- 2025-07-26 — `33183fb` — Update dependency eslint to v9.32.0 (#186)
- 2025-07-26 — `23692ab` — Fix using typed nil context values
- 2025-07-26 — `2473433` — Fix tests
- 2025-07-27 — `aa5edef` — Simplify context value checks. Add tags for debug cache logging.
- 2025-07-27 — `e351ed6` — Allow changing log level in runtime
- 2025-07-28 — `a804f76` — Update integrations and use Hugo-like data files
- 2025-07-28 — `0df58f7` — Fix typo
- 2025-07-28 — `6f74dbb` — Update gitignore [ci skip]
- 2025-07-28 — `f60bdcf` — Tweak gitignore [ci skip]
- 2025-07-31 — `3a2f916` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.40.1 (#187)
- 2025-07-31 — `85f4370` — Cosmetic improvement

## v0.0.6 — 2025-08-18

_Range: `v0.0.5..v0.0.6` · 49 commits_

- 2025-08-01 — `9f76007` — Update module github.com/prometheus/client_golang to v1.23.0 (#188)
- 2025-08-07 — `47d8e3f` — Update renovate.json
- 2025-08-11 — `a6be38e` — Add site script compatibility mode with Google reCAPTCHA
- 2025-08-11 — `91edc9c` — Add jitter to retry-after header. closes PrivateCaptcha/issues#170
- 2025-08-11 — `81fd002` — Generate checksum for server binary
- 2025-08-11 — `3f4baa7` — Update golang Docker tag to v1.24.6
- 2025-08-11 — `2bd0d93` — Add missing change
- 2025-08-12 — `fce5436` — Run maintenance job mutually exclusively locally
- 2025-08-13 — `953e13a` — Fix test dockerfile build
- 2025-08-14 — `fc8c04f` — Add notifications support
- 2025-08-14 — `2f816ad` — Use days for API keys expiration instead of months
- 2025-08-14 — `6d525bd` — Disambiguate URLs in email templates
- 2025-08-14 — `178e0bb` — Cosmetic improvements
- 2025-08-14 — `ff00d6f` — Add notifications for expiring API keys. closes PrivateCaptcha/issues#1
- 2025-08-14 — `edcd74e` — Pin golangci-lint Go version
- 2025-08-14 — `785ddfa` — Explicitly check terms and conditions checkbox. closes PrivateCaptcha/issues#150
- 2025-08-14 — `4e910e5` — Add SonarCloud coverage badge [ci skip]
- 2025-08-14 — `76052f1` — Prefix env config with PC_
- 2025-08-14 — `fb92733` — Add .NET integration metadata [ci skip]
- 2025-08-15 — `8863178` — Wrap few goroutines with recover
- 2025-08-15 — `92b35eb` — Introduce concept of persistent user notifications
- 2025-08-15 — `3884c97` — Improve welcome email contents
- 2025-08-15 — `bd821b8` — Safeguard batch callback instead of main routine
- 2025-08-15 — `e9e2990` — Cosmetic improvement
- 2025-08-15 — `cd907e0` — Safeguard levels backfill
- 2025-08-15 — `3ddd4c2` — Bump widget lib version
- 2025-08-15 — `97af84c` — Add delete record triggers to few tables
- 2025-08-15 — `9a6dfdc` — Simplify notifications code
- 2025-08-15 — `ce0c50f` — Introduce offboard user job
- 2025-08-15 — `4ae499b` — Don't export raw email HTML
- 2025-08-16 — `25b81f3` — Get rid of unnecessary config
- 2025-08-16 — `335ea6d` — Store also plain-text version of notification template in DB
- 2025-08-16 — `7751d0a` — Normalize maintenance job names
- 2025-08-16 — `6c76c34` — Add maintenance job to delete trial accounts
- 2025-08-16 — `a68b604` — Enforce user limiter also for API key middleware
- 2025-08-16 — `1e9a00f` — Add widget strings translations to few European languages
- 2025-08-16 — `6231572` — Move portal mailer to portal package
- 2025-08-16 — `25bf16e` — Attempt to guess first name for welcome email
- 2025-08-16 — `1484d3c` — Add from parameter to expired trials query
- 2025-08-16 — `bdf68b8` — Downgrade user notifications logging level
- 2025-08-16 — `2aabb90` — Add requires_subscription flag for notifications
- 2025-08-17 — `945100b` — Add circuit breaker for notifications processing
- 2025-08-17 — `8c8cfaf` — Add maintenance job to expire internal trials
- 2025-08-17 — `7b76518` — Add check for locked jobs intervals
- 2025-08-17 — `4da1357` — Add test for notification condition
- 2025-08-17 — `b74eacd` — Cosmetic improvements
- 2025-08-18 — `665431a` — Add ability to pass arguments to maintenance jobs. closes PrivateCaptcha/issues#171
- 2025-08-18 — `bc556d6` — Decrease stub license job interval
- 2025-08-18 — `f599426` — Gate widget lib publish with GitHub environment

## v0.0.7 — 2025-08-20

_Range: `v0.0.6..v0.0.7` · 5 commits_

- 2025-08-19 — `6807d6f` — Silence system notification error
- 2025-08-20 — `f49ad8c` — Add PHP and Ruby to integrations page
- 2025-08-20 — `f9ea159` — Cache stub puzzles too
- 2025-08-20 — `d575c3f` — Trim PHP SVG file
- 2025-08-20 — `02d78eb` — Cosmetic improvement

## v0.0.8 — 2025-08-25

_Range: `v0.0.7..v0.0.8` · 26 commits_

- 2025-08-22 — `2e64113` — Remove unused code
- 2025-08-22 — `390a4d3` — Remove unused code leftover
- 2025-08-22 — `eee6b3a` — Update module golang.org/x/net to v0.43.0 (#190)
- 2025-08-22 — `af188f1` — Make email templates more type-safe
- 2025-08-22 — `584acce` — Make scheduled notifications type-safe too
- 2025-08-22 — `94f088e` — Make email templates parsing lazy
- 2025-08-22 — `7ccb707` — Fix linter error
- 2025-08-22 — `8a3fcf9` — Fix error rendering for signed-in versions
- 2025-08-22 — `aa85424` — Add unit test to pre-parse email templates
- 2025-08-23 — `387f595` — Use xid-based puzzle ID
- 2025-08-23 — `1d62e7f` — Start migration from allow_replay to max_replay_count logic
- 2025-08-23 — `cff1dc4` — Pass our logger to otter cache
- 2025-08-23 — `170fde6` — Update dependency eslint to v9.33.0 (#191)
- 2025-08-23 — `3ce94b0` — Add more choices to puzzle validity interval
- 2025-08-23 — `779ed6f` — Fix profiling setup
- 2025-08-23 — `a0ce451` — Switch to otter cache in leaky bucket manager
- 2025-08-23 — `affbb60` — Drop allow_replay column for properties
- 2025-08-23 — `0e806aa` — Fix ignoring property errors
- 2025-08-23 — `b975ccb` — Add more logs
- 2025-08-25 — `b902343` — Improve puzzle cache performance
- 2025-08-25 — `5657fa3` — Fix var leaky bucket initialization for bucket manager
- 2025-08-25 — `be6ad99` — Mark domain name required in portal. closes PrivateCaptcha/issues#179
- 2025-08-25 — `803b07d` — Protect maintenance job endpoints. closes PrivateCaptcha/issues#178
- 2025-08-25 — `a28a657` — Fix tests
- 2025-08-25 — `45c3863` — Add few more paranoid arguments checks for DB
- 2025-08-25 — `a52e6f3` — Fix tests

## v0.0.9 — 2025-09-03

_Range: `v0.0.8..v0.0.9` · 4 commits_

- 2025-08-26 — `3139e0c` — Fix calculating puzzle issue time
- 2025-08-26 — `5bf531b` — Refactor puzzle verification and issue out of api.Server
- 2025-08-27 — `0cb03e3` — Update dependency esbuild to v0.25.9
- 2025-09-03 — `8151214` — Use events instead of JS callbacks

## v0.0.10 — 2025-09-03

_Range: `v0.0.9..v0.0.10` · 8 commits_

- 2025-09-03 — `d17f9c6` — Refactor test structs for export
- 2025-09-03 — `e171136` — Fix healthcheck job being exclusive
- 2025-09-03 — `29f9f6e` — Fix typo after puzzle verifier refactoring
- 2025-09-03 — `668c9b0` — Cosmetic fixes
- 2025-09-03 — `6c887ea` — Cosmetic improvements
- 2025-09-03 — `05fd0a0` — Log base64 bytes decoded on error
- 2025-09-03 — `975d2dd` — Add more logs
- 2025-09-03 — `b7dc45b` — Cosmetic improvement

## v0.0.11 — 2025-09-12

_Range: `v0.0.10..v0.0.11` · 11 commits_

- 2025-09-05 — `d8d9946` — Add docs link for property settings
- 2025-09-05 — `3aeb631` — Use pclime instead of green color
- 2025-09-05 — `0f83263` — Update colors in info messages
- 2025-09-07 — `d689e50` — Update dependency eslint to v9.34.0 (#192)
- 2025-09-08 — `29789c6` — Update module github.com/golang-migrate/migrate/v4 to v4.19.0 (#193)
- 2025-09-11 — `74ea495` — Update module golang.org/x/sync to v0.17.0 (#194)
- 2025-09-12 — `cfbc134` — Add service tag to logs
- 2025-09-12 — `58b429c` — Validate org members also by ID
- 2025-09-12 — `81158a1` — Fix organization invitations
- 2025-09-12 — `64925cd` — Improve logging
- 2025-09-12 — `0dd2af6` — Refine logging level of some statements

## v0.0.12 — 2025-09-15

_Range: `v0.0.11..v0.0.12` · 16 commits_

- 2025-09-13 — `bc983cd` — Improve org invite test
- 2025-09-13 — `f842dc9` — Tag more service contexts
- 2025-09-13 — `be9f1da` — Cosmetic improvement
- 2025-09-13 — `697fb54` — Passthrough service context to persisting sessions
- 2025-09-14 — `642b02b` — Refactor session management
- 2025-09-14 — `beaadac` — Add session management tests
- 2025-09-14 — `1c3968d` — Pin node version for frontend-builder to avoid constant pulls
- 2025-09-14 — `578f078` — Add few safeguards against messing int32 IDs in arguments
- 2025-09-14 — `53f5f17` — Fix linter warning
- 2025-09-14 — `496f77e` — Bump golang docker builder to 1.25.1
- 2025-09-15 — `2a5b525` — Remove query leftovers
- 2025-09-15 — `67e0937` — Remove unused code
- 2025-09-15 — `ef31cd2` — Make cache key for API keys public
- 2025-09-15 — `e4597e0` — Cosmetic improvement
- 2025-09-15 — `52c6c08` — Cosmetic improvements for sessions
- 2025-09-15 — `a01b7ee` — Decrease sessions flush timeout to 30 sec

## v0.0.13 — 2025-09-24

_Range: `v0.0.12..v0.0.13` · 11 commits_

- 2025-09-20 — `67417eb` — Update module github.com/prometheus/client_golang to v1.23.2
- 2025-09-22 — `da61446` — Update dependency eslint to v9.35.0 (#196)
- 2025-09-23 — `a11e3e9` — Update module golang.org/x/crypto to v0.42.0 (#197)
- 2025-09-24 — `8b311b7` — Update module golang.org/x/net to v0.44.0 (#198)
- 2025-09-24 — `8de1219` — Print sitekey in the portal
- 2025-09-24 — `909f31a` — Restore default widget start mode to auto
- 2025-09-24 — `09c98a2` — Update pgx to v5.7.6
- 2025-09-24 — `5fdd094` — Fix sitekey layout in portal with localhost label
- 2025-09-24 — `81dc447` — Add ETag header for assets and scripts
- 2025-09-24 — `5afd43b` — Enable WordPress integration
- 2025-09-24 — `7fc5fa5` — Vary portal stylesheet with git commit hash

## v0.0.14 — 2025-09-30

_Range: `v0.0.13..v0.0.14` · 5 commits_

- 2025-09-26 — `c3335d3` — Make widget color configurable
- 2025-09-28 — `c3be50c` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.40.3
- 2025-09-30 — `ad3e1d3` — Relax empty user name requirement
- 2025-09-30 — `795845c` — Correctly check for existing internal subscriptions
- 2025-09-30 — `5496e4e` — Fix reading from cache during transaction

## v0.0.15 — 2025-09-30

_Range: `v0.0.14..v0.0.15` · 2 commits_

- 2025-09-30 — `8c8d2d8` — Update SonarQube Action to v6
- 2025-09-30 — `f173096` — Return updated user when creating account

## v0.0.16 — 2025-10-04

_Range: `v0.0.15..v0.0.16` · 9 commits_

- 2025-10-02 — `91d59f0` — Update dependency esbuild to v0.25.10
- 2025-10-04 — `70ed850` — Move XSRF key to config
- 2025-10-04 — `9642cef` — Ultra-cosmetic improvement
- 2025-10-04 — `4e221b2` — Check for org ownership when deleting members
- 2025-10-04 — `63d01b6` — Add max length check
- 2025-10-04 — `3bf698c` — Add extra validation for org name
- 2025-10-04 — `0b26d10` — Fix outdated signature bound check
- 2025-10-04 — `9a464a0` — Add out of bounds check for reading license activation
- 2025-10-04 — `ea0fb8c` — Cosmetic improvement

## v0.0.17 — 2025-10-04

_Range: `v0.0.16..v0.0.17` · 5 commits_

- 2025-10-04 — `1efa23b` — Validate user name server-side
- 2025-10-04 — `9498f8b` — Update dependency eslint to v9.36.0 (#199)
- 2025-10-04 — `c16e9c0` — Validate property name better server-side
- 2025-10-04 — `fbe38a2` — Add paranoid int32 checks to make GitHub CodeQL happy
- 2025-10-04 — `ca2b123` — Cosmetic security improvements

## v0.0.18 — 2025-10-06

_Range: `v0.0.17..v0.0.18` · 10 commits_

- 2025-10-04 — `0577ba7` — Add test to create a new property
- 2025-10-04 — `1577142` — Validate API key name
- 2025-10-04 — `3a8fc21` — Add test to create API key
- 2025-10-04 — `5ed9365` — Improve property creation test
- 2025-10-05 — `cc53a14` — Update dependency @tailwindcss/typography to v0.5.18
- 2025-10-06 — `5840f18` — Add scan-docker-image action to CI
- 2025-10-06 — `187523d` — Fix padding for docker scan params
- 2025-10-06 — `5a6e14b` — Fix wrong context for user notification cleanup
- 2025-10-06 — `907b63e` — Fix truncated API key in notification. closes PrivateCaptcha/issues#187
- 2025-10-06 — `8f91da3` — Cosmetic improvement

## v0.0.19 — 2025-10-08

_Range: `v0.0.18..v0.0.19` · 5 commits_

- 2025-10-08 — `4fa44c1` — Add properties options menu to show sitekeys in dashboard
- 2025-10-08 — `293e230` — Show basic challenge stats aggregated. closes PrivateCaptcha/issues#161
- 2025-10-08 — `1fd3596` — Fix portal login verifications missing from metrics
- 2025-10-08 — `98cd19d` — Add functionality to rotate API keys. closes PrivateCaptcha/issues#188
- 2025-10-08 — `0990a1a` — Cosmetic improvements

## v0.0.20 — 2025-10-11

_Range: `v0.0.19..v0.0.20` · 18 commits_

- 2025-10-08 — `f791647` — Update README.md
- 2025-10-09 — `8f48156` — Cosmetic improvements
- 2025-10-09 — `6064339` — Send email when invited to organization. closes PrivateCaptcha/issues#96
- 2025-10-09 — `f9ef189` — Add CE label for signed-out header and footer
- 2025-10-10 — `0a3c965` — Fix dark theme override for checkbox
- 2025-10-10 — `2d15d1f` — Check number of orgs owned by user. closes PrivateCaptcha/issues#196
- 2025-10-10 — `cba58f6` — Cosmetic improvement
- 2025-10-10 — `f9d3524` — Rephrase widget js error message
- 2025-10-10 — `f430c84` — Force widget font size override for external styles interference
- 2025-10-10 — `10e6d60` — Revert "Force widget font size override for external styles interference"
- 2025-10-10 — `5df983a` — Fix invisible widget
- 2025-10-10 — `b9ebb58` — Bump widget corelib version
- 2025-10-10 — `fe1b40b` — Add widget test
- 2025-10-10 — `1579720` — Wait for event with timeout
- 2025-10-10 — `5fbe31c` — Bump happy-dom to 15.x.x
- 2025-10-10 — `048ddd9` — Cosmetic improvement
- 2025-10-11 — `b23ab1f` — Fix misaligned property levels in settings
- 2025-10-11 — `698ca5c` — Add more event tests to widget

## v0.0.21 — 2025-10-21

_Range: `v0.0.20..v0.0.21` · 27 commits_

- 2025-10-11 — `2c16f4d` — Update dependency happy-dom to v20 [SECURITY] (#202)
- 2025-10-11 — `6ae42a8` — Add feature to widget to update data-styles
- 2025-10-11 — `0b09a49` — Bump widget lib version
- 2025-10-12 — `365f253` — Fix immutable stylesheet error when updating widget styles
- 2025-10-12 — `ce7b82d` — Bump widget corelib version
- 2025-10-12 — `1fc4440` — Verify styles changed before updating them
- 2025-10-13 — `57f788f` — Backfill stub puzzle access records
- 2025-10-14 — `77f20ea` — Update dependency @tailwindcss/typography to v0.5.19
- 2025-10-16 — `d07f488` — Bump happy-dom in /widget in the npm_and_yarn group across 1 directory (#203)
- 2025-10-16 — `ff694e1` — Switch to Golang 1.25.3
- 2025-10-16 — `6237e2b` — Add Go 1.25 built-in Cross-Origin protection middleware to Portal
- 2025-10-16 — `a05a756` — Add cosmetic spot check for the most stupid bots
- 2025-10-16 — `2da712f` — Bump golangci-lint
- 2025-10-16 — `f48466c` — Bump golangci-lint version
- 2025-10-16 — `7bf291f` — Bump golangci-lint to 2.5
- 2025-10-17 — `f93082b` — Update Node.js to v22.20.0 (#204)
- 2025-10-18 — `4b19a81` — Update dependency eslint to v9.37.0 (#205)
- 2025-10-18 — `350868d` — Save solution only when user clicks widget. closes PrivateCaptcha/issues#202
- 2025-10-19 — `b55492d` — Reduce rate limit for catchall path
- 2025-10-19 — `ff19bc4` — Improve rate limit update for catchall
- 2025-10-19 — `14b17c7` — Only update rate limit for non-portal catchall
- 2025-10-19 — `f2fa6f8` — Replace rate limit update to smaller limits for catchall
- 2025-10-20 — `21df85e` — Fix issues with widget
- 2025-10-21 — `d0355fa` — Add auto mode for widget language
- 2025-10-21 — `794b620` — Cosmetic JS improvements
- 2025-10-21 — `16305e2` — Do not remove onFocusIn event handler
- 2025-10-21 — `6533940` — Add explicit reset method to workers pool

## v0.0.22 — 2025-11-13

_Range: `v0.0.21..v0.0.22` · 34 commits_

- 2025-10-22 — `e296845` — Update dependency tailwindcss to v3.4.18
- 2025-10-23 — `bc19269` — Update module golang.org/x/crypto to v0.43.0 (#207)
- 2025-10-24 — `4f21d66` — Update module golang.org/x/net to v0.46.0 (#206)
- 2025-10-26 — `19acfe7` — Fix code style issues (#208)
- 2025-10-26 — `d80834c` — Enable ruby
- 2025-10-30 — `375e1ec` — Update dependency esbuild to v0.25.11
- 2025-11-01 — `a822e91` — Update dependency happy-dom to v20.0.5
- 2025-11-02 — `98a4b68` — Update dependency eslint to v9.38.0 (#210)
- 2025-11-03 — `0b813d0` — Update dependency happy-dom to v20.0.7
- 2025-11-05 — `88106ee` — Update dependency happy-dom to v20.0.8
- 2025-11-06 — `1669284` — Update Node.js to v22.21.0 (#211)
- 2025-11-09 — `7dc6f65` — Add a separate metrics for before-redirect http errors in portal
- 2025-11-09 — `03ff4c2` — Attempt to improve home-baked errors tracking. related PrivateCaptcha/issues#206
- 2025-11-09 — `bc3d941` — Add link to API keys from integration settings. closes PrivateCaptcha/issues#208
- 2025-11-09 — `d1aa1f2` — Add Java integration pending
- 2025-11-09 — `f7460e0` — Add retry for fetching chart data
- 2025-11-09 — `efcdb4c` — Add resend timer. closes PrivateCaptcha/issues#205
- 2025-11-09 — `b5af8ae` — Fix tests
- 2025-11-09 — `26f812c` — Update module golang.org/x/sync to v0.18.0 (#212)
- 2025-11-11 — `a016ef1` — Add crossorigin attribute to portal scripts. related PrivateCaptcha/issues#206
- 2025-11-11 — `3eb0853` — Allow build-time redefining activation URL for selfhosted licenses
- 2025-11-11 — `c1f4a82` — Add retry for fetching usage chart data
- 2025-11-11 — `b406781` — Cosmetic improvement
- 2025-11-11 — `71cdc7f` — Add option to run a single integration test from make
- 2025-11-11 — `9b3aa0e` — Add functionality to move property. closes PrivateCaptcha/issues#151
- 2025-11-11 — `a43f816` — Expose couple of DB methods to deal with subscriptions
- 2025-11-11 — `73346ec` — Add external_email column to subscriptions
- 2025-11-12 — `d789ff0` — Set enterprise flag for tests
- 2025-11-12 — `345ba53` — Add domain label to property dashboard
- 2025-11-12 — `f54578d` — Hide internal identifiers in URLs. closes PrivateCaptcha/issues#209
- 2025-11-13 — `7a5405a` — Cosmetic improvements
- 2025-11-13 — `fec1537` — Add fallback for empty ID hasher salt
- 2025-11-13 — `798dfbc` — Potential fix for code scanning alert no. 20: Incorrect conversion between integer types
- 2025-11-13 — `fa927a2` — Fix CodeQL suggestion

## v0.0.23 — 2025-11-20

_Range: `v0.0.22..v0.0.23` · 28 commits_

- 2025-11-14 — `d2bbf77` — Update dependency happy-dom to v20.0.10
- 2025-11-15 — `5f9cefc` — Update dependency eslint to v9.39.0 (#214)
- 2025-11-15 — `43a01e8` — Add more details to 2fa email. closes PrivateCaptcha/issues#207
- 2025-11-15 — `5807771` — Add header config to tests too
- 2025-11-15 — `ac0993b` — Add 2fa details to plain text message. related PrivateCaptcha/issues#207
- 2025-11-15 — `e9443ab` — Cosmetic improvement
- 2025-11-15 — `11d7363` — Update module github.com/tsenart/vegeta/v12 to v12.13.0 (#215)
- 2025-11-16 — `89128a6` — Remove /twofactor endpoint and unite all login forms
- 2025-11-16 — `e6b68a2` — Fix linter warning
- 2025-11-16 — `16e4244` — Add functionality to backfill potential stale session in portal
- 2025-11-16 — `97cbe24` — Fix linter warning
- 2025-11-16 — `5e1f835` — Enable registration for CI
- 2025-11-17 — `043fe94` — Update dependency esbuild to v0.25.12
- 2025-11-18 — `18445c5` — Update dependency eslint to v9.39.1
- 2025-11-19 — `f8c1bbb` — Add log for limited users
- 2025-11-19 — `15a9082` — Allow organization members to access verify API for property
- 2025-11-19 — `0ae7d84` — Split user limiter into properties and API
- 2025-11-19 — `cf374ab` — Add more logs for requests cut by user limiter
- 2025-11-19 — `c2990f5` — Correct misspellings (#217)
- 2025-11-19 — `81e01d8` — Include property org members for limit checks backfill
- 2025-11-19 — `cc697aa` — Cosmetic improvement
- 2025-11-19 — `27d5b03` — Add more configuration options for memory cache
- 2025-11-19 — `884a5d2` — Force recheck user limits upon joining the org
- 2025-11-19 — `f1013af` — Add backpressure for org member checks
- 2025-11-20 — `aa77a30` — Add dependabot to CLA exceptions
- 2025-11-20 — `b8d969c` — Bump js-yaml in /widget in the npm_and_yarn group across 1 directory (#216)
- 2025-11-20 — `f8d815f` — Bump golang.org/x/crypto in the go_modules group across 1 directory (#218)
- 2025-11-20 — `6726ec7` — Remove unused code

## v0.0.24 — 2025-11-29

_Range: `v0.0.23..v0.0.24` · 43 commits_

- 2025-11-22 — `fa8422d` — Add basic auditlogs implementation. closes PrivateCaptcha/issues#204
- 2025-11-23 — `1e550a0` — Bump glob from 10.3.10 to 10.5.0 in /web in the npm_and_yarn group across 1 directory (#219)
- 2025-11-23 — `06ecf0f` — Skip deleting expired internal trials
- 2025-11-23 — `3e52d9a` — Use user email from join rather than current user
- 2025-11-23 — `4ad20a5` — Cosmetic improvements
- 2025-11-23 — `ad1a7fc` — Fix duplicate ID
- 2025-11-23 — `f4643f8` — Export cache helper
- 2025-11-23 — `fbc9623` — Show properties and orgs usage in Usage tab
- 2025-11-23 — `2962ca9` — Cosmetic fix
- 2025-11-23 — `23f56d9` — Count only user-owned orgs for stats screen
- 2025-11-24 — `d8e9740` — Tune down sitekey cached errors
- 2025-11-24 — `2d67bc1` — Add more logs to sitekey endpoint for errors
- 2025-11-24 — `07b5c24` — Add ability to set TTL when loading through cache
- 2025-11-24 — `036959e` — Update dependency esbuild to ^0.27.0 (#220)
- 2025-11-24 — `4ac5039` — Keep subscription audit events separate from user
- 2025-11-24 — `90ee511` — Log audit events without user ID
- 2025-11-24 — `4661c50` — Export query key string helper
- 2025-11-25 — `7b77fc2` — Cosmetic improvements
- 2025-11-25 — `907b733` — Cosmetic improvement
- 2025-11-25 — `b7753e9` — Fix linter error
- 2025-11-25 — `ba4c278` — Add few more nil checks for audit log parsing
- 2025-11-25 — `bbd6d26` — Cosmetic improvement
- 2025-11-25 — `66d2ef8` — Use different audit logs cleanup days for EE
- 2025-11-26 — `feb9432` — Fix code style issues (#221)
- 2025-11-26 — `6a7a23c` — Add empty state template for audit logs
- 2025-11-26 — `2c51345` — Cosmetic improvement
- 2025-11-26 — `a5b14c5` — Tweak min height
- 2025-11-26 — `d73fff8` — Use a separate login job
- 2025-11-26 — `2c87015` — Export login job
- 2025-11-26 — `e0f92e9` — Allow specifying root path for Postgres migrations
- 2025-11-26 — `b0bbb39` — Make audit logs retention configurable
- 2025-11-26 — `89ca31f` — Make audit logs context overridable
- 2025-11-26 — `ee9ab73` — Export core implementation of audit logs func
- 2025-11-26 — `747d162` — Allow HTML in messages
- 2025-11-27 — `cfc3636` — Fix indexing when there're no audit logs
- 2025-11-27 — `c7b682a` — Fix see more button
- 2025-11-27 — `fdd3d7d` — Add passthrough API headers
- 2025-11-27 — `43f6d07` — Fix docker volume for Postgres 18
- 2025-11-27 — `69eb310` — Fallback to RemoteAddr parsing for IP rate limit
- 2025-11-29 — `f1766fd` — Bump session TTL to 3h
- 2025-11-29 — `7a921f8` — Fix auto-start mode in popup widget mode
- 2025-11-29 — `3ac40d0` — Bump widget lib package version to 16
- 2025-11-29 — `cc94c89` — Skip duplicate user started and execute triggers

## v0.0.25 — 2025-12-03

_Range: `v0.0.24..v0.0.25` · 14 commits_

- 2025-12-01 — `96ae0b9` — Update module github.com/PuerkitoBio/goquery to v1.11.0
- 2025-12-01 — `a1ee804` — Merge pull request #222 from PrivateCaptcha/renovate/github.com-puerkitobio-goquery-1.x
- 2025-12-01 — `54ddecb` — Record cancellation in subscription audit event
- 2025-12-02 — `da93cf5` — Check expected sitekey in /verify
- 2025-12-02 — `8cc7b28` — Update postgres 18 in tests docker
- 2025-12-02 — `14fa539` — Add more user-friendly http error body response
- 2025-12-02 — `c2c3fa9` — Cosmetic improvements
- 2025-12-02 — `1ac836c` — Fix tests
- 2025-12-02 — `cbab265` — Send verify response instead of http code for sitekey verification
- 2025-12-03 — `008c5be` — Fix tests
- 2025-12-03 — `e928ab2` — Update OpenAPI spec
- 2025-12-03 — `33d1372` — Fix OpenAPI linter
- 2025-12-03 — `58b0573` — Add comment to flaky test
- 2025-12-03 — `916bb28` — Add separate test for verifying test property with another sitekey

## v0.0.26 — 2025-12-19

_Range: `v0.0.25..v0.0.26` · 66 commits_

- 2025-12-04 — `b23d735` — Update Node.js to v22.21.1
- 2025-12-05 — `4fbf64b` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.41.0
- 2025-12-05 — `fbe5007` — Refactor subscription limits. related PrivateCaptcha/issues#142
- 2025-12-05 — `79e4226` — Move limit getters to limits interface. related PrivateCaptcha/issues#142
- 2025-12-05 — `773d08e` — Export field for inheritance
- 2025-12-05 — `1eb0cdb` — Expose count checked in the limit
- 2025-12-05 — `f0044c7` — Return difference from limit check
- 2025-12-05 — `f03ab19` — Move logging out of limits checks
- 2025-12-05 — `e4bf6b1` — Merge pull request #223 from PrivateCaptcha/renovate/github.com-clickhouse-clickhouse-go-v2-2.x
- 2025-12-05 — `7eeddd4` — Make Makefile more friendly to Podman
- 2025-12-05 — `0e69844` — Cosmetic improvement
- 2025-12-05 — `6954099` — Bump golang to 1.25.5
- 2025-12-06 — `e03bf4f` — Check if lock exists before trying to obtain it
- 2025-12-06 — `567c7dd` — Bump sqlc to 1.30.0
- 2025-12-07 — `5537757` — Cache chart stats responses on CDN level
- 2025-12-07 — `9151c22` — Cache chart stats also on the server level
- 2025-12-08 — `cf56507` — Skip caching user stats on CDN level
- 2025-12-08 — `81e5652` — Fix linter error
- 2025-12-08 — `57636bb` — Add API key scope
- 2025-12-08 — `f0db3be` — Add audit log source
- 2025-12-08 — `e6847f2` — Fix build
- 2025-12-08 — `ec0b65c` — Shuffle auditlog table columns
- 2025-12-08 — `e8bf52f` — Move RouteGenerator to common
- 2025-12-08 — `96da407` — Fix typo
- 2025-12-08 — `9effd7a` — Refactor API server routes mounting
- 2025-12-08 — `03910a8` — Shuffle limits related code to DB
- 2025-12-08 — `7c433a9` — Fix build
- 2025-12-08 — `261c27a` — Fix tests
- 2025-12-10 — `3b20824` — Fix typo
- 2025-12-10 — `85ca8c5` — Add preliminary basic version of orgs API. related PrivateCaptcha/issues#45
- 2025-12-11 — `fa61bb9` — Show scroll in orgs dropdown
- 2025-12-12 — `a384a1f` — Update dependency happy-dom to v20.0.11
- 2025-12-13 — `97f71cc` — Fix total element not defined. closes PrivateCaptcha/issues#231
- 2025-12-14 — `fd94846` — Update module github.com/golang-migrate/migrate/v4 to v4.19.1
- 2025-12-15 — `b959c00` — Add create properties bulk API. related PrivateCaptcha/issues#45
- 2025-12-15 — `2fe3fe6` — Cosmetic improvements
- 2025-12-15 — `c42eef5` — Fix badge links in README.md
- 2025-12-15 — `3bcd76e` — Remove another unused variable
- 2025-12-15 — `6084818` — Move Alpine data storage higher
- 2025-12-17 — `653b91f` — Add API support to delete properties. related PrivateCaptcha/issues#45
- 2025-12-17 — `bbdf596` — Remove refactoring leftover
- 2025-12-17 — `b0c520e` — Fix new API errors metric namespace
- 2025-12-17 — `b0c7b4c` — Cosmetic improvement
- 2025-12-17 — `05e6536` — Add pagination support for org properties. related PrivateCaptcha/issues#45
- 2025-12-17 — `d1ac04d` — Cosmetic improvements
- 2025-12-17 — `66d8424` — Split portal and DB page size for properties
- 2025-12-17 — `a01792f` — Add API to get properties. related PrivateCaptcha/issues#45
- 2025-12-17 — `50945a6` — Fix typo
- 2025-12-18 — `e91ed9d` — Update module golang.org/x/sync to v0.19.0
- 2025-12-18 — `8386d16` — Cosmetic improvement
- 2025-12-18 — `25b289e` — Merge pull request #227 from PrivateCaptcha/renovate/golang.org-x-sync-0.x
- 2025-12-18 — `3475c9c` — Add API to batch-update properties. related PrivateCaptcha/issues#45
- 2025-12-18 — `6e6174b` — Fix cosmetic GitHub Code Quality findings
- 2025-12-18 — `f446e28` — Add API to get a single property. related PrivateCaptcha/issues#45
- 2025-12-18 — `def6a5b` — Add new Portal APIs to OpenAPI file
- 2025-12-19 — `c6ddaf7` — Improve OpenAPI spec
- 2025-12-19 — `77cb6e9` — Cleanup user caches on logout
- 2025-12-19 — `24b43ad` — Refactor email verification
- 2025-12-19 — `9c69d83` — Generate 2FA code with better rng
- 2025-12-19 — `3f68566` — Bump widget attempts
- 2025-12-19 — `c45dbd4` — Validate property chart period on the client
- 2025-12-19 — `207a28e` — Fix build
- 2025-12-19 — `a25fd05` — Cosmetic improvements
- 2025-12-19 — `207d8a0` — Fix api key scope migration
- 2025-12-19 — `f308e55` — Make properties API to use PUT
- 2025-12-19 — `fe8fe81` — Bump widget lib version to 18

## v0.0.27 — 2025-12-23

_Range: `v0.0.26..v0.0.27` · 14 commits_

- 2025-12-20 — `55f23e9` — Update dependency esbuild to v0.27.1
- 2025-12-22 — `1d34cea` — Move to trusted publishing for corelib package
- 2025-12-22 — `495c3ea` — Add Makefile variable for NPM publish
- 2025-12-22 — `727fbfa` — Preallocate map size
- 2025-12-22 — `782dbb5` — Validate API requests while reading JSON
- 2025-12-22 — `3c36323` — Use node v24 for widget publishing
- 2025-12-22 — `1478f7a` — Add read-only attribute to the scope of API key
- 2025-12-22 — `cbdc994` — Add Org scope to API keys. closes PrivateCaptcha/issues#237
- 2025-12-23 — `bf9aa3d` — Update module golang.org/x/net to v0.48.0
- 2025-12-23 — `d048d71` — Merge pull request #233 from PrivateCaptcha/renovate/golang.org-x-net-0.x
- 2025-12-23 — `a113c35` — Add new fields to audit event for API keys
- 2025-12-23 — `998a7b1` — Rename puzzle scope to captcha in UI
- 2025-12-23 — `6e1eef1` — Fix test
- 2025-12-23 — `cbca4f3` — Update otter to 2.3.0

## v1.28.0 — 2026-01-27

_Range: `v0.0.27..v1.28.0` · 116 commits_

- 2025-12-23 — `612a14f` — Add org scope error
- 2025-12-25 — `acc2e09` — Add timeout config for periodic jobs
- 2025-12-25 — `4c98278` — Do not show org select for non-EE edition
- 2025-12-25 — `53f8146` — Hide portal API key scope for non-EE
- 2025-12-26 — `efb767c` — Update dependency tailwindcss to v3.4.19
- 2025-12-27 — `c1cf621` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.42.0
- 2025-12-27 — `c5b5929` — Merge pull request #235 from PrivateCaptcha/renovate/github.com-clickhouse-clickhouse-go-v2-2.x
- 2025-12-27 — `094dfdd` — Fix code style issues (#234)
- 2025-12-28 — `c0b8234` — Update dependency eslint to v9.39.2
- 2025-12-31 — `06d9abc` — Update dependency esbuild to v0.27.2
- 2026-01-01 — `c2c55f4` — Update dependency @tailwindcss/forms to v0.5.11
- 2026-01-02 — `748d0e9` — Add migrate and serve mode
- 2026-01-02 — `8112359` — Add more tests
- 2026-01-02 — `d1a2376` — Enforce 2FA code expiration timeout before session timeout
- 2026-01-02 — `0e7ef0c` — Register time.Time for 2FA code expiration serialization
- 2026-01-02 — `05ecc9c` — Cosmetic improvements
- 2026-01-02 — `55a6a17` — Add SECURITY.md
- 2026-01-04 — `787f1d4` — Add missing 2FA timestamp to context
- 2026-01-05 — `ae44851` — Update module github.com/medama-io/go-useragent to v1.2.3
- 2026-01-06 — `6cd042b` — Remove .env file requirement from tests
- 2026-01-06 — `f534ee7` — Add a mode to run tests without ClickHouse
- 2026-01-06 — `5a4abcb` — Add AGENTS.md
- 2026-01-06 — `2435770` — Add tests for in-memory time series
- 2026-01-06 — `da33bdc` — Update AGENTS.md
- 2026-01-06 — `3147816` — Add init target for environment
- 2026-01-06 — `82a578f` — Cosmetic improvement
- 2026-01-07 — `2abcf09` — Add more instructions for LLMs
- 2026-01-07 — `8a03c3e` — Exclude Copilot bot from CLA
- 2026-01-07 — `55d9612` — Add more portal tests (#237)
- 2026-01-07 — `3f67ea1` — Bump checkout action to v6 and nodejs version to 24
- 2026-01-07 — `f0958d2` — Add copilot setup steps
- 2026-01-07 — `d7089d7` — Remove cache option for node setup
- 2026-01-07 — `23a4e11` — Update Node.js to v24 (#239)
- 2026-01-07 — `d00d9ce` — Add unit and integration tests for code coverage improvements (#238)
- 2026-01-07 — `18ec971` — Cosmetic improvements
- 2026-01-07 — `b9a3945` — Add negative codepath tests for API endpoints (#242)
- 2026-01-08 — `bb82daa` — Make global variables in tests consistent
- 2026-01-08 — `a519cf0` — Cosmetic improvements
- 2026-01-08 — `ae0972b` — Fix build
- 2026-01-08 — `0e2323c` — Add copilot
- 2026-01-08 — `bc5f8a9` — Add test coverage for maintenance jobs, rate limiter, cache, and portal handlers (#241)
- 2026-01-08 — `7d12478` — Add organization transfer feature (#240)
- 2026-01-08 — `19991f1` — Cosmetic improvements
- 2026-01-08 — `7dc8891` — Fix duplicate html element ID
- 2026-01-09 — `3e453bd` — Cosmetic improvement
- 2026-01-09 — `336353e` — Add negative test coverage for portal endpoint
- 2026-01-09 — `f02b7fc` — Remove unused code
- 2026-01-10 — `8442b9e` — Allow inviting users without accounts. closes PrivateCaptcha/issues#227
- 2026-01-10 — `2c98081` — Improve org invitation functionality. related PrivateCaptcha/issues#227
- 2026-01-10 — `341d071` — Add cosmetic validation for email domains
- 2026-01-10 — `79965c7` — Fix tests
- 2026-01-10 — `3cbcb9b` — Update module github.com/jackc/pgx/v5 to v5.8.0 (#245)
- 2026-01-10 — `27495ae` — Cosmetic improvements
- 2026-01-10 — `5926536` — Revert redirect 'fix'
- 2026-01-10 — `b13d351` — Cosmetic improvement [ci skip]
- 2026-01-10 — `3f3c098` — Add more tests
- 2026-01-11 — `17f577a` — Generate coverage report as an artifact
- 2026-01-11 — `f3dd1fa` — Use portal mailer in tests instead of stub mailer
- 2026-01-11 — `e1f0e61` — Use standard timeout handler
- 2026-01-11 — `ebdeb3e` — Cosmetic improvement
- 2026-01-11 — `9f1c81a` — Fix typo
- 2026-01-11 — `688ace5` — Cosmetic improvement
- 2026-01-12 — `c9da66d` — Use backoff and multiple attempts to ping DBs
- 2026-01-12 — `00c287f` — Bump readiness drain delay
- 2026-01-12 — `d1c0a1a` — Split timeout handler into soft and hard
- 2026-01-12 — `8b16043` — Use http request with context
- 2026-01-12 — `5cffff0` — Use status recorder also for recovered middleware
- 2026-01-13 — `76b9b81` — Add ErrorLog for http server
- 2026-01-13 — `2a1e074` — fix: guard against custom element 'progress-ring' already existing (#250)
- 2026-01-13 — `411a289` — Protect against private-captcha redeclaration
- 2026-01-13 — `ba1dcb1` — Cosmetic improvements
- 2026-01-14 — `28ba7ed` — Create html element explicitly
- 2026-01-14 — `ec0aca8` — Cosmetic improvement
- 2026-01-14 — `0ceb759` — Refactor html string concatenation to JS in widget html
- 2026-01-14 — `7d1ef63` — Add t.Helper annotations to Go test helpers (#253)
- 2026-01-15 — `e09db1d` — Allow org members without subscription to create properties via API (#251)
- 2026-01-15 — `0f70c38` — Add more tests
- 2026-01-15 — `809a667` — Allow localhost subdomains
- 2026-01-15 — `64ceb48` — Remove unused code
- 2026-01-15 — `5e8eac3` — Bump widget lib version to 19
- 2026-01-16 — `0e55045` — Move default profile settings to a correct file in ClickHouse
- 2026-01-16 — `808f979` — Fix widget thickness
- 2026-01-17 — `205b556` — Bump widget library version to 20
- 2026-01-17 — `cd536f5` — Make progress component safer
- 2026-01-17 — `9f101a1` — Split settings usage stats by organization (#256)
- 2026-01-20 — `cab8ae7` — Fix widget reset code
- 2026-01-20 — `4ab2092` — Update cla.yml
- 2026-01-20 — `091e6d3` — Make audit log parsing extensible for downstream repositories (#258)
- 2026-01-20 — `a8d0148` — Add widget JS unit tests for reset() and public API (#259)
- 2026-01-20 — `4691924` — Cosmetic cleanup [ci skip]
- 2026-01-20 — `4f8d2a9` — Respect context cancellation in backoff wait
- 2026-01-20 — `78210af` — Bump widget lib version to 22
- 2026-01-22 — `d2ef95e` — Update dependency happy-dom to v20.1.0 (#260)
- 2026-01-22 — `894b884` — Add http timeout for license activation
- 2026-01-23 — `7d42e32` — Remove unused code
- 2026-01-23 — `9048bca` — Add timeouts for channel selects
- 2026-01-23 — `e72cf3c` — Record last used time for API keys. closes PrivateCaptcha/issues#166
- 2026-01-23 — `56b86be` — Add missing enum for test [ci skip]
- 2026-01-23 — `d2867e7` — Rename drop metric location
- 2026-01-24 — `48c28a9` — Update module golang.org/x/text to v0.33.0 (#263)
- 2026-01-24 — `8a86a43` — Fix typos
- 2026-01-25 — `f98e54b` — Enable Java integration
- 2026-01-26 — `e8999c9` — Move to bash scripts for DB init in Docker
- 2026-01-26 — `4f69181` — Fix CI
- 2026-01-26 — `5b06e34` — Fix CI
- 2026-01-26 — `c662aa3` — Add option to have admin pwd for ClickHouse
- 2026-01-26 — `3c3f31a` — Fix heredoc in bash
- 2026-01-26 — `d355541` — Cosmetic improvement
- 2026-01-26 — `315248b` — Fix CI
- 2026-01-26 — `354967a` — Bump ClickHouse version to 25.6.2
- 2026-01-27 — `acae441` — Improve test coverage for puzzle, portal, monitoring packages (#264)
- 2026-01-27 — `a780209` — Add load test for verify endpoint. closes PrivateCaptcha/issues#98
- 2026-01-27 — `2a73f2d` — Use easyjson for marshaling json. related PrivateCaptcha/issues#98
- 2026-01-27 — `d640316` — Add lint command [ci skip]
- 2026-01-27 — `dafc0c0` — Exclude easyjson generated files in sonar-project.properties
- 2026-01-27 — `b7665ba` — Update README.md [ci skip]

## v1.28.1 — 2026-01-28

_Range: `v1.28.0..v1.28.1` · 1 commit_

- 2026-01-28 — `a2ee4e2` — Update docker.yaml

## v1.28.2 — 2026-01-28

_Range: `v1.28.1..v1.28.2` · 5 commits_

- 2026-01-28 — `80a3ad4` — Print sha256 sum of public keys for verifcation
- 2026-01-28 — `8e9cca9` — Mark server images read-only in docker compose
- 2026-01-28 — `a3dd64f` — Do not close DB connections after migration in auto mode
- 2026-01-28 — `bbbe766` — Move more API types to easyjson
- 2026-01-28 — `e5bfd64` — Add ability to send verify requests for test puzzle in loadtest

## v1.29.0 — 2026-02-09

_Range: `v1.28.2..v1.29.0` · 50 commits_

- 2026-01-29 — `70d3bf9` — Cosmetic improvement
- 2026-01-30 — `6d405ee` — Add 2FA code grace time
- 2026-02-01 — `1dd81d1` — Update dependency happy-dom to v20.3.1
- 2026-02-01 — `9a8f08c` — Update module golang.org/x/crypto to v0.47.0
- 2026-02-01 — `6b267f3` — Merge pull request #265 from PrivateCaptcha/renovate/happy-dom-monorepo
- 2026-02-01 — `6706927` — Update Node.js to v24.13.0
- 2026-02-01 — `de2b8d3` — Update module golang.org/x/net to v0.49.0 (#267)
- 2026-02-01 — `7df17b4` — Merge pull request #268 from PrivateCaptcha/renovate/node-24.x
- 2026-02-02 — `6d2ebcf` — Update dependency happy-dom to v20.3.3
- 2026-02-03 — `916fd03` — Update dependency happy-dom to v20.3.4
- 2026-02-04 — `5cb2b83` — Make timeout configurable for handlers
- 2026-02-04 — `859e3c1` — Initial plan
- 2026-02-04 — `0fd0674` — Initial exploration
- 2026-02-04 — `271b986` — Add fetch timeout functionality and unit test
- 2026-02-04 — `779ee62` — Fix code review comments in test and puzzle.js
- 2026-02-04 — `681cd9e` — Merge pull request #271 from PrivateCaptcha/copilot/add-fetch-timeout-configuration
- 2026-02-04 — `fe7d44f` — Initial plan
- 2026-02-04 — `53f3938` — Add global timeout (30s) for fetch operations and per-call timeout (5s)
- 2026-02-04 — `bf60284` — Consolidate fetchWithBackoff arguments to options object and simplify test
- 2026-02-04 — `0d44d8e` — Clarify test comment about timeout threshold
- 2026-02-04 — `6ce5f8e` — Merge pull request #272 from PrivateCaptcha/copilot/add-global-timeout-option
- 2026-02-04 — `f4e03a3` — Cosmetic improvement
- 2026-02-04 — `a21d07b` — Fix route handler last path not getting into monitoring middleware
- 2026-02-05 — `7722efb` — Initial plan
- 2026-02-05 — `b1641e3` — Add Enabled field to Property with API and Portal integration
- 2026-02-05 — `460a414` — Fix template sitekey reference and remove duplicate enabled check
- 2026-02-05 — `069475f` — Address PR feedback: simplify HTML, rename error, update API/SQL
- 2026-02-05 — `a306142` — Add tests for disabled property codepaths
- 2026-02-05 — `3a0aef7` — Fix failing tests and format test files properly
- 2026-02-05 — `c4e8c3e` — Fix failing integration tests by clearing cache properly
- 2026-02-05 — `2fe0c7f` — Fix TestGetOrgPropertyDisabled by clearing property cache
- 2026-02-05 — `f9073ed` — Merge pull request #273 from PrivateCaptcha/copilot/add-enabled-field-to-property
- 2026-02-05 — `9ba20f6` — Initial plan
- 2026-02-05 — `b678ca1` — Initial plan
- 2026-02-05 — `fbb7ad4` — Add functionality to disable users
- 2026-02-05 — `ea4906a` — Fix property disabling
- 2026-02-05 — `a866478` — Replace mutex with semaphore for maintenance job concurrency control (#275)
- 2026-02-06 — `49f9efd` — Cosmetic improvement
- 2026-02-07 — `9ced6af` — Update dependency happy-dom to v20.3.7 (#276)
- 2026-02-07 — `ff6a78b` — Add masked IP address field to audit logs (#277)
- 2026-02-08 — `d893d88` — Take maintenance mode into account for /ready endpoint
- 2026-02-08 — `644b8b0` — Cosmetic improvement
- 2026-02-08 — `2728667` — Fix test
- 2026-02-08 — `7ac3c78` — Add license check for community edition too
- 2026-02-08 — `8160fe7` — Fix tests
- 2026-02-09 — `20e5c18` — Show unlicensed state in the UI
- 2026-02-09 — `4ca981a` — Add circuit breaker for invalid sitekeys
- 2026-02-09 — `bfd7d4d` — Revert few logs due to nil check
- 2026-02-09 — `5730a9e` — Add license type parameter for activation
- 2026-02-09 — `a1580a6` — Fix portal tests

## v1.30.0 — 2026-03-02

_Range: `v1.29.0..v1.30.0` · 47 commits_

- 2026-02-09 — `97e7941` — Update dependency happy-dom to v20.3.9
- 2026-02-10 — `755959a` — Abstract away property interface to support rules
- 2026-02-11 — `dc75cfd` — Pass IP down the login job for audit
- 2026-02-11 — `a2e6d0a` — Add panic metric
- 2026-02-11 — `8f4509a` — Bump Go to 1.25.7
- 2026-02-11 — `af6f8f0` — Update dependency happy-dom to v20.4.0 (#280)
- 2026-02-11 — `8e69551` — Fix nil dereference error
- 2026-02-13 — `5592db7` — Update module github.com/ClickHouse/clickhouse-go/v2 to v2.43.0 (#283)
- 2026-02-13 — `505a8f5` — Add git hook to format and vet files
- 2026-02-13 — `07bf5f6` — Fix go vet command
- 2026-02-13 — `0db0153` — Run go fmt
- 2026-02-13 — `2f7e801` — Exclude vendors from checking go fmt
- 2026-02-14 — `2ecebf5` — Add simple utility to browse error logs
- 2026-02-14 — `f5edbfb` — Add makefile command to find the two-factor code from logs
- 2026-02-14 — `52e62d0` — Fix linter warnings
- 2026-02-14 — `e7c1dea` — Refactor formatlogs
- 2026-02-14 — `31b2c3c` — Improve compatibility with running on a single domain with prefixes
- 2026-02-14 — `5559aaa` — Refactor render constants
- 2026-02-15 — `19afacf` — Show internal error on widget in debug mode
- 2026-02-15 — `4369b23` — Skip SSL mode for integration tests
- 2026-02-15 — `ddaecc8` — Revert "Refactor render constants"
- 2026-02-15 — `80cc35e` — Cosmetic improvements
- 2026-02-15 — `50b2021` — Do not use relurl for return URL
- 2026-02-16 — `cd69609` — Add support of tab navigation via URL argument in portal
- 2026-02-17 — `758b9ea` — Consolidate some error messages
- 2026-02-19 — `09fd27e` — Use async insert in ClickHouse
- 2026-02-19 — `8618b7d` — Fix linter error
- 2026-02-22 — `cc1c120` — Run unit tests on pre-push
- 2026-02-22 — `902e465` — Revert "Run unit tests on pre-push"
- 2026-02-26 — `5639fb9` — Add govulncheck job to CI (#324)
- 2026-02-26 — `c07e62f` — Move govulncheck to a separate action
- 2026-02-26 — `c905c14` — Cosmetic improvements
- 2026-02-27 — `7f2a4c6` — Fix org invite: show login page on re-access by same user, fix email invite deletion (#325)
- 2026-03-01 — `c02505c` — Fix difficulty threshold computations on the client
- 2026-03-01 — `d3ff4c0` — Revert threshold computation in JS
- 2026-03-01 — `90c4c6f` — Fix pre-commit hook compatibility with macOS bash 3.2 / zsh (#326)
- 2026-03-02 — `8f82143` — Revert "Revert threshold computation in JS"
- 2026-03-02 — `c3cda2b` — Migrate difficulty levels after client fix
- 2026-03-02 — `ff99db3` — Use 30 seconds for email resend
- 2026-03-02 — `7dff9aa` — Cosmetic improvements
- 2026-03-02 — `d88730d` — Safeguard DB puzzle difficulty
- 2026-03-02 — `1cde52d` — Set min difficulty level in API too
- 2026-03-02 — `801f43e` — Fix tests
- 2026-03-02 — `6c09b97` — Correct difficulty level
- 2026-03-02 — `3485fce` — Adjust difficulty growth curve
- 2026-03-02 — `49aa4cd` — Adjust difficulty growth curve
- 2026-03-02 — `1fcafed` — Use difference cache-control header for script

## v1.30.1 — 2026-03-03

_Range: `v1.30.0..v1.30.1` · 11 commits_

- 2026-03-02 — `beeb44a` — Bump core widget lib version
- 2026-03-02 — `6960c21` — Add rollback for ClickHouse failures
- 2026-03-02 — `0ca7c61` — Cosmetic improvement
- 2026-03-03 — `aad817f` — Make Solve method cancelable via context (#327)
- 2026-03-03 — `376e3fc` — Cosmetic improvement
- 2026-03-03 — `2dca103` — Improve solver performance
- 2026-03-03 — `7b45b77` — Correctly take context error into account for solver
- 2026-03-03 — `e72bfc4` — Fix tests
- 2026-03-03 — `a619002` — Reread items with StoreBulkReader on cache refresh
- 2026-03-03 — `5edee17` — Cache test property API response for less time
- 2026-03-03 — `9f07d10` — Use different solutions count for stub and non-stub properties

## v1.30.2 — 2026-03-04

_Range: `v1.30.1..v1.30.2` · 1 commit_

- 2026-03-04 — `b2a0dd2` — Fix unconditional rollback for ClickHouse

## v1.30.3 — 2026-03-10

_Range: `v1.30.2..v1.30.3` · 25 commits_

- 2026-03-04 — `c8c0a73` — Add x-cloak to all x-show elements in layouts (#329)
- 2026-03-05 — `0f4c742` — Handle context.Canceled in Portal Handler and API error responses (#335)
- 2026-03-05 — `5ec5709` — Cosmetic improvements
- 2026-03-05 — `783f739` — Add Postgres connection metrics
- 2026-03-05 — `6303ec1` — Split portal and API metrics handlers to fix interface override
- 2026-03-05 — `2c38083` — Remove halucinated option
- 2026-03-05 — `45a1f4f` — Optimize pre-commit hook
- 2026-03-05 — `371bfb4` — Update clickhouse/clickhouse-server Docker tag to v26 (#288)
- 2026-03-06 — `bb87b89` — Update dependency esbuild to v0.27.3
- 2026-03-06 — `4b10266` — Update dependency happy-dom to v20.6.3 (#342)
- 2026-03-07 — `a80ae42` — chore: upgrade Go to 1.25.8 (#345)
- 2026-03-07 — `20368bb` — Update dependency eslint to v9.39.3 (#346)
- 2026-03-07 — `2c7ff77` — Update Node.js to v24.13.1 (#347)
- 2026-03-07 — `4279ce5` — Bump Go to 1.26.1 (#348)
- 2026-03-07 — `71ddae4` — Bump golangci-lint version
- 2026-03-08 — `36da48c` — fix(deps): update module golang.org/x/crypto to v0.48.0 (#352)
- 2026-03-08 — `2e8fbee` — chore(deps): update dependency happy-dom to v20.7.0 (#351)
- 2026-03-09 — `422101a` — Truncate SQL query args in trace logs via slog.LogValuer (#356)
- 2026-03-09 — `845ea0c` — Use max of request and verify logs in RetrieveAccountStats (#355)
- 2026-03-09 — `456cb9f` — Update golang.org/x/net to v0.51.0
- 2026-03-09 — `7337f8a` — Use slog Logger field instead of deprecated Debug/Debugf in connectClickhouse (#357)
- 2026-03-09 — `c467e7b` — Remove final from account stats query
- 2026-03-10 — `48f462a` — fix(deps): update module golang.org/x/sync to v0.20.0 (#365)
- 2026-03-10 — `a5728c2` — Extract licensed tag to another file
- 2026-03-10 — `8c856ce` — Disable period buttons during chart fetch to prevent out-of-order responses (#370)

## v1.31.0 — 2026-03-17

_Range: `v1.30.3..v1.31.0` · 29 commits_

- 2026-03-11 — `dfdd821` — Add coderabbit config [ci skip]
- 2026-03-11 — `8819232` — chore(deps): update node.js to v24.14.0 (#373)
- 2026-03-11 — `36b73de` — Show production notification template IDs in \`viewemails\` (#375)
- 2026-03-12 — `a0e2e27` — Improve GuessFirstName function
- 2026-03-12 — `e61807c` — Cosmetic improvements
- 2026-03-12 — `c4ab8fd` — Add ability to override From and ReplyTo for notifications
- 2026-03-12 — `934ea36` — Add direct unit coverage for \`isSkipName\` and \`isAllCaps\` (#379)
- 2026-03-12 — `c05d083` — Only use ReplyTo override together with EmailFrom
- 2026-03-12 — `3c8966f` — Fix tests when cache-eviction for sessions happens
- 2026-03-13 — `325d0ff` — Handle CreateUserNotification unique constraint conflicts gracefully (#384)
- 2026-03-13 — `efc843d` — Cosmetic improvements
- 2026-03-13 — `d03e07d` — Make few cache keys public
- 2026-03-14 — `28fcbf1` — Print error log summary in console
- 2026-03-14 — `c38c24a` — Align standalone ClickHouse runner with repository-pinned image version (#387)
- 2026-03-14 — `131e6f0` — chore(deps): update clickhouse/clickhouse-server docker tag to v26.2.1 (#385)
- 2026-03-15 — `9cea703` — Increase cache size
- 2026-03-15 — `728faee` — Add difficulty rules system. closes PrivateCaptcha/issues#135
- 2026-03-16 — `e10d529` — Fix security issues
- 2026-03-16 — `27f1406` — Fix IDOR in difficulty rule mutations (delete/update/move) (#391)
- 2026-03-16 — `68c64bd` — Potential fixes for 2 code quality findings (#393)
- 2026-03-16 — `e8cde01` — Fix leakybucket.Manager data races in Update and SetGlobalLimits (#392)
- 2026-03-16 — `6d34351` — Allow to override reply to separately
- 2026-03-16 — `3a6d6f1` — Potential fixes for 2 code quality findings (#395)
- 2026-03-16 — `d51bc23` — Don't log 2fa code
- 2026-03-16 — `bd5cd06` — Decrease logging error
- 2026-03-16 — `4451ba8` — Add "always" difficulty condition property that unconditionally matches all requests (#396)
- 2026-03-16 — `e125b76` — Decrease logging errors
- 2026-03-16 — `c5e177f` — Fix typo
- 2026-03-17 — `b9080a4` — chore(deps): update dependency happy-dom to v20.8.3 (#397)

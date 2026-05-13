.PHONY: clean build deploy

STAGE ?= dev
GIT_COMMIT ?= $(shell git rev-list -1 HEAD)
DOCKER_IMAGE ?= private-captcha
DOCKER ?= docker
SQLC_MIGRATION_FIX = pkg/db/migrations/postgres/000000_sqlc_fix.sql
EXTRA_BUILD_FLAGS ?=
EXTRA_TEST_FLAGS ?=
CGO_TEST_ENABLED ?= 0
TEST_NAME ?=
TEST_DOCKER_COMPOSE_FILES ?= -f docker/docker-compose.test.yml -f docker/docker-compose.test.clickhouse.yml
GOPATH := $(shell go env GOPATH)
OPEN ?= printf "file://%s\n"

setup-git:
	git config core.hooksPath scripts/hooks
	git config commit.cleanup whitespace

docker/pc.env: docker/pc.env.example
	cp -v docker/pc.env.example docker/pc.env

setup-docker: docker/pc.env

lint:
	$(GOPATH)/bin/golangci-lint run

init-widget:
	cd widget && env STAGE="$(STAGE)" npm install

init-web:
	cd web && env STAGE="$(STAGE)" npm install

test-unit:
	@env GOFLAGS="-mod=vendor" CGO_ENABLED=$(CGO_TEST_ENABLED) go test $(EXTRA_TEST_FLAGS) -tags enterprise -short ./...

test-unit-race: EXTRA_TEST_FLAGS = -race
test-unit-race: CGO_TEST_ENABLED = 1
test-unit-race: test-unit

test-unit-cover:
	@env GOFLAGS="-mod=vendor" CGO_ENABLED=$(CGO_TEST_ENABLED) go test $(EXTRA_TEST_FLAGS) -tags enterprise -short -coverprofile=coverage_unit.cov -coverpkg=$(shell go list ./... | paste -sd, -) ./...

test-unit-cover-race: EXTRA_TEST_FLAGS = -race
test-unit-cover-race: CGO_TEST_ENABLED = 1
test-unit-cover-race: test-unit-cover

test-widget-unit:
	cd widget && env STAGE="$(STAGE)" npm run test

bench-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -bench=. -benchtime=20s -short ./...

test-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose $(TEST_DOCKER_COMPOSE_FILES) down -v --remove-orphans
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose $(TEST_DOCKER_COMPOSE_FILES) run --build --remove-orphans --rm migration
	@env GIT_COMMIT="$(GIT_COMMIT)" TEST_NAME="$(TEST_NAME)" $(DOCKER) compose $(TEST_DOCKER_COMPOSE_FILES) up --build --abort-on-container-exit --remove-orphans --force-recreate testserver
	@mkdir -p coverage_integration
	@$(DOCKER) compose $(TEST_DOCKER_COMPOSE_FILES) cp testserver:/app/coverage_reports/. coverage_integration/
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose $(TEST_DOCKER_COMPOSE_FILES) down -v --remove-orphans

test-docker-light: TEST_DOCKER_COMPOSE_FILES = -f docker/docker-compose.test.yml
test-docker-light: test-docker

vendors:
	go mod tidy
	go mod vendor

build: build-server build-loadtest build-view-emails build-view-widget build-puzzledbg

build-tests:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=$(CGO_TEST_ENABLED) go test $(EXTRA_TEST_FLAGS) -c -cover -covermode=atomic $(EXTRA_BUILD_FLAGS) -o tests/ $(shell go list $(EXTRA_BUILD_FLAGS) -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...) -coverpkg=$(shell go list $(EXTRA_BUILD_FLAGS) ./... | paste -sd, -)

build-tests-ee: EXTRA_BUILD_FLAGS = -tags enterprise
build-tests-ee: build-tests

build-tests-ee-race: EXTRA_TEST_FLAGS = -race
build-tests-ee-race: CGO_TEST_ENABLED = 1
build-tests-ee-race: build-tests-ee

build-server:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w -X main.GitCommit=$(GIT_COMMIT)" $(EXTRA_BUILD_FLAGS) -o bin/server ./cmd/server

build-server-profile: EXTRA_BUILD_FLAGS = -tags profile -gcflags=all=-N
build-server-profile: build-server

build-server-ee: EXTRA_BUILD_FLAGS = -tags enterprise
build-server-ee: build-server

build-loadtest:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/loadtest cmd/loadtest/*.go

build-puzzledbg:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/puzzledbg cmd/puzzledbg/*.go

deploy:
	@echo "Deploy target is not implemented. Please use your CI/CD pipeline or add deployment steps here."
	@false

build-docker:
	$(DOCKER) build -f ./docker/Dockerfile --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(DOCKER_IMAGE):latest .

build-js:
	rm -fv web/static/js/* || echo 'Nothing to remove'
	cd web && env STAGE="$(STAGE)" npm run build

build-widget-script:
	rm -fv widget/static/js/* || echo 'Nothing to remove'
	cd widget && env STAGE="$(STAGE)" npm run build

build-widget-library:
	rm -fv widget/lib/*.js widget/lib/*.js.map || echo 'Nothing to remove'
	cd widget && env STAGE="$(STAGE)" BUILD_TARGET="library" npm run build

publish-widget-library: EXTRA_PUBLISH_FLAGS = --access public
publish-widget-library:
	cd widget/lib && npm publish $(EXTRA_PUBLISH_FLAGS)

build-view-emails:
	env GOFLAGS="-mod=vendor" go build -o bin/viewemails cmd/viewemails/*.go

build-view-portal:
	env GOFLAGS="-mod=vendor" go build -tags "enterprise viewportal" -o bin/viewportal cmd/viewportal/*.go

build-view-widget:
	env GOFLAGS="-mod=vendor" go build -o bin/viewwidget cmd/viewwidget/*.go

generate-easyjson:
	# NOTE: api package has to be first because portal imports it
	# might require commenting out init() method in pkg/api/server.go
	GOFLAGS=-mod=mod $(GOPATH)/bin/easyjson pkg/api/response.go
	GOFLAGS=-mod=mod $(GOPATH)/bin/easyjson pkg/portal/response.go

copy-static-js:
	cp -v web/js/index.js web/static/js/bundle.js
	cp -v web/js/htmx.min.js web/static/js/
	cp -v web/js/alpine.min.js web/static/js/
	cp -v web/js/alpine.persist.min.js web/static/js/
	cp -v web/js/d3.v7.min.js web/static/js/

init: init-widget init-web build-js build-widget-script copy-static-js

serve: build-js build-widget-script copy-static-js build-server
	bin/server

run:
	reflex -r '^(pkg|cmd|vendor|web)/' -R '^(web/static/js|web/node_modules)' -s -- sh -c 'make serve'

run-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.local.yml up --build
	@$(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix 2>/dev/null | ./scripts/error-logs-summary.sh || true
	@$(OPEN) "$$( $(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix | go run cmd/formatlogs/main.go )"

run-docker-ce:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.local.yml -f docker/docker-compose.ce.yml up --build
	@$(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix 2>/dev/null | ./scripts/error-logs-summary.sh || true
	@$(OPEN) "$$( $(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix | go run cmd/formatlogs/main.go )"

run-docker-ee:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.local.yml -f docker/docker-compose.ee.yml up --build
	@$(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix 2>/dev/null | ./scripts/error-logs-summary.sh || true
	@$(OPEN) "$$( $(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix | go run cmd/formatlogs/main.go )"

profile-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.monitoring.yml up --build

watch-docker:
	@$(DOCKER) compose -f docker/docker-compose.base.yml watch

clean-docker:
	@$(DOCKER) compose -f docker/docker-compose.base.yml down -v --remove-orphans

find-docker-2fa:
	@$(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix 2>/dev/null | jq -r 'select(.msg=="Failed to send two factor code") | .code' | tail -n 1

sqlc:
	# https://github.com/sqlc-dev/sqlc/issues/3571
	echo "CREATE SCHEMA backend;" > $(SQLC_MIGRATION_FIX)
	cd pkg/db && sqlc generate --no-remote
	go run cmd/clumpbools/main.go -w pkg/db/generated/models.go
	gofmt -w pkg/db/generated/models.go
	rm -v $(SQLC_MIGRATION_FIX)

vet-sqlc:
	# https://github.com/sqlc-dev/sqlc/issues/3571
	echo "CREATE SCHEMA backend;" > $(SQLC_MIGRATION_FIX)
	cd pkg/db && sqlc vet
	rm -v $(SQLC_MIGRATION_FIX)

vet-docker:
	@$(DOCKER) compose -f docker/docker-compose.test.yml run --build --remove-orphans --rm vetsqlc

view-docker-logs: OPEN = open
view-docker-logs:
	@$(OPEN) "$$( $(DOCKER) compose -f docker/docker-compose.base.yml logs server --no-log-prefix | go run cmd/formatlogs/main.go )"

view-emails: build-view-emails
	bin/viewemails

run-view-emails:
	reflex -r '^(pkg\/email|cmd\/viewemails)/' -s -- sh -c 'make view-emails'

view-portal: build-js build-widget-script copy-static-js build-view-portal
	bin/viewportal

run-view-portal:
	reflex -r '^(pkg\/portal|web|cmd\/viewportal)/' \
		-R '^(web/static/js|web/node_modules)' \
		-s -- sh -c 'make view-portal'

view-widget: NOTICE =
view-widget: build-js build-widget-script build-view-widget
	bin/viewwidget $(if $(NOTICE),-notice "$(NOTICE)")

run-view-widget:
	reflex -r '^(widget|web|cmd\/viewwidget)/' \
		-R '^(web/static/js|widget/static/js|widget/node_modules|web/node_modules)' \
		-s -- sh -c 'make view-widget'

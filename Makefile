.PHONY: clean build deploy

STAGE ?= dev
GIT_COMMIT ?= $(shell git rev-list -1 HEAD)
DOCKER_IMAGE ?= private-captcha
DOCKER ?= docker
SQLC_MIGRATION_FIX = pkg/db/migrations/postgres/000000_sqlc_fix.sql
EXTRA_BUILD_FLAGS ?=
TEST_NAME ?=

test-unit:
	@env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -tags enterprise -short ./...

test-unit-cover:
	@env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -tags enterprise -short -coverprofile=coverage_unit.cov -coverpkg=$(shell go list ./... | paste -sd, -) ./...

test-widget-unit:
	cd widget && env STAGE="$(STAGE)" npm run test

bench-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -bench=. -benchtime=20s -short ./...

test-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.test.yml down -v --remove-orphans
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.test.yml run --build --remove-orphans --rm migration
	@env GIT_COMMIT="$(GIT_COMMIT)" TEST_NAME="$(TEST_NAME)" $(DOCKER) compose -f docker/docker-compose.test.yml up --build --abort-on-container-exit --remove-orphans --force-recreate testserver
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.test.yml down -v --remove-orphans

vendors:
	go mod tidy
	go mod vendor

build: build-server build-loadtest build-view-emails build-view-widget build-puzzledbg

build-tests:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -cover -covermode=atomic $(EXTRA_BUILD_FLAGS) -o tests/ $(shell go list $(EXTRA_BUILD_FLAGS) -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...) -coverpkg=$(shell go list $(EXTRA_BUILD_FLAGS) ./... | paste -sd, -)

build-tests-ee: EXTRA_BUILD_FLAGS = -tags enterprise
build-tests-ee: build-tests

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
	echo "Nothing here"

build-docker:
	$(DOCKER) build -f ./docker/Dockerfile --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(DOCKER_IMAGE):latest .

build-js:
	rm -v web/static/js/* || echo 'Nothing to remove'
	cd web && env STAGE="$(STAGE)" npm run build

build-widget-script:
	rm -v widget/static/js/* || echo 'Nothing to remove'
	cd widget && env STAGE="$(STAGE)" npm run build

build-widget-library:
	rm -v widget/lib/*.js widget/lib/*.js.map || echo 'Nothing to remove'
	cd widget && env STAGE="$(STAGE)" BUILD_TARGET="library" npm run build

publish-widget-library:
	cd widget/lib && npm publish --access public

build-view-emails:
	env GOFLAGS="-mod=vendor" go build -o bin/viewemails cmd/viewemails/*.go

build-view-widget:
	env GOFLAGS="-mod=vendor" go build -o bin/viewwidget cmd/viewwidget/*.go

copy-static-js:
	cp -v web/js/index.js web/static/js/bundle.js
	cp -v web/js/htmx.min.js web/static/js/
	cp -v web/js/alpine.min.js web/static/js/
	cp -v web/js/alpine.persist.min.js web/static/js/
	cp -v web/js/d3.v7.min.js web/static/js/

serve: build-js build-widget-script copy-static-js build-server
	bin/server

run:
	reflex -r '^(pkg|cmd|vendor|web)/' -R '^(web/static/js|web/node_modules)' -s -- sh -c 'make serve'

run-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.local.yml up --build

run-docker-ee:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.local.yml -f docker/docker-compose.ee.yml up --build

profile-docker:
	@env GIT_COMMIT="$(GIT_COMMIT)" $(DOCKER) compose -f docker/docker-compose.base.yml -f docker/docker-compose.monitoring.yml up --build

watch-docker:
	@$(DOCKER) compose -f docker/docker-compose.base.yml watch

clean-docker:
	@$(DOCKER) compose -f docker/docker-compose.base.yml down -v --remove-orphans

sqlc:
	# https://github.com/sqlc-dev/sqlc/issues/3571
	echo "CREATE SCHEMA backend;" > $(SQLC_MIGRATION_FIX)
	cd pkg/db && sqlc generate --no-remote
	rm -v $(SQLC_MIGRATION_FIX)

vet-sqlc:
	# https://github.com/sqlc-dev/sqlc/issues/3571
	echo "CREATE SCHEMA backend;" > $(SQLC_MIGRATION_FIX)
	cd pkg/db && sqlc vet
	rm -v $(SQLC_MIGRATION_FIX)

vet-docker:
	@$(DOCKER) compose -f docker/docker-compose.test.yml run --build --remove-orphans --rm vetsqlc

view-emails: build-view-emails
	bin/viewemails

run-view-emails:
	reflex -r '^(pkg\/email|cmd\/viewemails)/' -s -- sh -c 'make view-emails'

view-widget: build-js build-widget-script build-view-widget
	bin/viewwidget

run-view-widget:
	reflex -r '^(widget|web|cmd\/viewwidget)/' \
		-R '^(web/static/js|widget/static/js|widget/node_modules|web/node_modules)' \
		-s -- sh -c 'make view-widget'

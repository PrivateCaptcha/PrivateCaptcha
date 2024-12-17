.PHONY: clean build deploy

STAGE ?= dev
GIT_COMMIT ?= $(shell git rev-list -1 HEAD)
DOCKER_IMAGE ?= private-captcha

test-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -v -short ./...

bench-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -bench=. -benchtime=20s -short ./...

test-docker:
	@docker compose -f docker/docker-compose.test.yml down -v --remove-orphans
	@docker compose -f docker/docker-compose.test.yml run --build --remove-orphans --rm migration
	@docker compose -f docker/docker-compose.test.yml up --build --abort-on-container-exit --remove-orphans --force-recreate testserver
	@docker compose -f docker/docker-compose.test.yml down -v --remove-orphans

vendors:
	go mod tidy
	go mod vendor

build: build-server build-loadtest

build-tests:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -c -o tests/ ./...

build-server:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w -X main.GitCommit=$(GIT_COMMIT)" -o bin/server cmd/server/*.go

build-loadtest:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/loadtest cmd/loadtest/*.go

deploy:
	echo "Nothing here"

build-docker:
	docker build -f ./docker/Dockerfile --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(DOCKER_IMAGE):latest .

build-js:
	rm -v web/static/js/* || echo 'Nothing to remove'
	cd web && npm run build

build-widget:
	rm -v widget/static/js/* || echo 'Nothing to remove'
	cd widget && npm run build

build-view-emails:
	env GOFLAGS="-mod=vendor" go build -o bin/viewemails cmd/viewemails/*.go

build-view-widget:
	env GOFLAGS="-mod=vendor" go build -o bin/viewwidget cmd/viewwidget/*.go

copy-static-js:
	cp -v web/js/index.js web/static/js/bundle.js
	cp -v web/js/htmx.min.js web/static/js/
	cp -v web/js/alpine.min.js web/static/js/
	cp -v web/js/d3.v7.min.js web/static/js/

serve: build-js build-widget copy-static-js build-server
	bin/server

run:
	reflex -r '^(pkg|cmd|vendor|web)/' -R '^(web/static/js|web/node_modules)' -s -- sh -c 'make serve'

run-docker:
	@docker compose -f docker/docker-compose.local.yml up --build

profile-docker:
	@docker compose -f docker/docker-compose.local.yml -f docker/docker-compose.monitoring.yml up --build

watch-docker:
	@docker compose -f docker/docker-compose.local.yml watch

clean-docker:
	@docker compose -f docker/docker-compose.local.yml down -v --remove-orphans

sqlc:
	cd pkg/db && sqlc generate --no-remote

vet-sqlc:
	cd pkg/db && sqlc vet

vet-docker:
	@docker compose -f docker/docker-compose.test.yml run --build --remove-orphans --rm vetsqlc

view-emails: build-view-emails
	bin/viewemails

run-view-emails:
	reflex -r '^(pkg\/email|cmd\/viewemails)/' -s -- sh -c 'make view-emails'

view-widget: build-js build-widget build-view-widget
	bin/viewwidget

run-view-widget:
	reflex -r '^(widget|web|cmd\/viewwidget)/' \
		-R '^(web/static/js|widget/static/js|widget/node_modules|web/node_modules)' \
		-s -- sh -c 'make view-widget'

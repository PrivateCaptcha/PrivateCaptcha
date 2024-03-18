.PHONY: clean build deploy

STAGE ?= dev
GIT_COMMIT ?= $(shell git rev-list -1 HEAD)
DOCKER_IMAGE ?= private-captcha

test-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -v -short ./...

bench-unit:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -bench=. -benchtime=6s -short ./...

test-integration:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go test -v ./...

test-docker:
	@docker compose -f docker/docker-compose.test.yml down -v
	@docker compose -f docker/docker-compose.test.yml up --build --abort-on-container-exit --remove-orphans --force-recreate
	@docker compose -f docker/docker-compose.test.yml down -v

vendors:
	go mod tidy
	go mod vendor

build: build-server

build-server:
	env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -ldflags="-s -w -X main.GitCommit=$(GIT_COMMIT)" -o bin/server cmd/server/*.go

deploy:
	echo "Nothing here"

build-docker:
	docker build -f ./docker/Dockerfile --build-arg GIT_COMMIT=$(GIT_COMMIT) -t $(DOCKER_IMAGE):latest .

build-js:
	rm -v web/static/js/* || echo 'Nothing to remove'
	cd web && npm run build

serve: build-js build-server
	bin/server

run:
	reflex -r '^(pkg|cmd|vendor|web)/' -R '^(web/static/js|web/node_modules)' -s -- sh -c 'make serve'

run-docker:
	@docker compose -f docker/docker-compose.yml up --build

watch-docker:
	@docker compose -f docker/docker-compose.yml watch

clean-docker:
	@docker compose -f docker/docker-compose.yml down -v

sqlc:
	cd pkg/db && sqlc generate

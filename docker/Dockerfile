# Stage 1: Frontend build
FROM node:latest AS frontend-builder

# has to be done first because `npm install` fails otherwise
WORKDIR /app/web

# Install dependencies and build the frontend
COPY ./web/package.json /app/web/package.json
RUN --mount=type=cache,target=/cache/npm npm install

# copy the rest
COPY ./web /app/web

RUN --mount=type=cache,target=/cache/npm,sharing=locked npm run build

# Stage 2: Backend build
FROM golang:1.22.1 AS backend-builder

# Copy the backend source code
COPY ./go.mod ./go.sum /app/
COPY ./vendor /app/vendor
COPY ./cmd/server /app/cmd/server
COPY ./pkg /app/pkg
COPY ./web /app/web

COPY --from=frontend-builder /app/web/static /app/web/static

# "Build" JS (will possibly be replaced by esbuild/webpack in future)
COPY --from=frontend-builder /app/web/js/index.js /app/web/static/js/bundle.js
COPY --from=frontend-builder /app/web/js/htmx.min.js /app/web/static/js/
COPY --from=frontend-builder /app/web/js/alpine.min.js /app/web/static/js/
COPY --from=frontend-builder /app/web/js/d3.v7.min.js /app/web/static/js/

WORKDIR /app

RUN mkdir bin

ENV GOMODCACHE=/cache/gomod
ENV GOCACHE=/cache/gobuild

ARG GIT_COMMIT=HEAD
RUN --mount=type=cache,target=/cache/gomod --mount=type=cache,target=/cache/gobuild,sharing=locked env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -C cmd/server -ldflags="-s -w -X main.GitCommit=${GIT_COMMIT}" -o ../../bin/server

# Final stage: Production container
FROM golang:1.22.1

COPY --from=backend-builder /app/bin/server /app/server

ENV PC_HOST=0.0.0.0
ENV PC_PORT=8080
EXPOSE 8080

CMD ["/app/server", "-mode", "server"]

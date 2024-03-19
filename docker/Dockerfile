# Stage 1: Frontend build
FROM node:latest AS frontend-builder

COPY ./web /app/web

WORKDIR /app/web

# Install dependencies and build the frontend
RUN npm install
RUN npm run build

# Stage 2: Backend build
FROM golang:latest AS backend-builder

# Copy the backend source code
COPY . /app/

COPY --from=frontend-builder /app/web/static /app/web/static

# "Build" JS (will possibly be replaced by esbuild/webpack in future)
COPY --from=frontend-builder /app/web/js/index.js /app/web/static/js/bundle.js
COPY --from=frontend-builder /app/web/js/htmx.min.js /app/web/static/js/htmx.min.js

WORKDIR /app

RUN mkdir bin

ARG GIT_COMMIT=HEAD
RUN env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -C cmd/server -ldflags="-s -w -X main.GitCommit=${GIT_COMMIT}" -o ../../bin/server

# Final stage: Production container
FROM golang:latest

COPY --from=backend-builder /app/bin/server /app/server

ENV PC_HOST 0.0.0.0
ENV PC_PORT 8080
EXPOSE 8080

CMD ["/app/server"]

# Stage 1: Frontend build
FROM node:22.20.0 AS frontend-builder

#############################################
############# Building web ##################
#############################################
# has to be done first because `npm install` fails otherwise
WORKDIR /app/web

# Install dependencies and build the frontend
COPY ./web/package.json /app/web/package.json
RUN --mount=type=cache,target=/cache/web npm install --verbose --ignore-scripts

# copy the rest
COPY ./web /app/web

RUN --mount=type=cache,target=/cache/web,sharing=locked npm run build


################################################
############# Building widget ##################
################################################
# has to be done first because `npm install` fails otherwise
WORKDIR /app/widget

# Install dependencies and build the frontend
COPY ./widget/package.json /app/widget/package.json
RUN --mount=type=cache,target=/cache/widget npm install --verbose --ignore-scripts

# copy the rest
COPY ./widget /app/widget

ARG STAGE
ENV STAGE=${STAGE}
RUN --mount=type=cache,target=/cache/widget,sharing=locked npm run build

################################################
############# Building backend #################
################################################

# Stage 2: Backend build
FROM golang:1.25.3 AS backend-builder

# Copy the backend source code
COPY ./go.mod ./go.sum /app/
COPY ./vendor /app/vendor
COPY ./cmd/server /app/cmd/server
COPY ./pkg /app/pkg
COPY ./web /app/web
COPY ./widget /app/widget

COPY --from=frontend-builder /app/web/static /app/web/static
COPY --from=frontend-builder /app/widget/static /app/widget/static

# "Build" JS (will possibly be replaced by esbuild/webpack in future)
COPY --from=frontend-builder /app/web/js/index.js /app/web/static/js/bundle.js
COPY --from=frontend-builder /app/web/js/htmx.min.js /app/web/static/js/
COPY --from=frontend-builder /app/web/js/alpine.min.js /app/web/static/js/
COPY --from=frontend-builder /app/web/js/alpine.persist.min.js /app/web/static/js/
COPY --from=frontend-builder /app/web/js/d3.v7.min.js /app/web/static/js/

WORKDIR /app

RUN mkdir bin

ENV GOMODCACHE=/cache/gomod
ENV GOCACHE=/cache/gobuild

ARG GIT_COMMIT=HEAD
ARG EXTRA_BUILD_FLAGS=
ARG GO_LDFLAGS="-s -w"
RUN --mount=type=cache,target=/cache/gomod --mount=type=cache,target=/cache/gobuild,sharing=locked env GOFLAGS="-mod=vendor" CGO_ENABLED=0 go build -C cmd/server -ldflags="${GO_LDFLAGS} -X main.GitCommit=${GIT_COMMIT}" ${EXTRA_BUILD_FLAGS} -o ../../bin/server

# Final stage: Production container
FROM gcr.io/distroless/static-debian12

COPY --from=backend-builder /app/bin/server /app/server

ENV PC_HOST=0.0.0.0
ENV PC_PORT=8080
EXPOSE 8080

CMD ["/app/server", "-mode", "server"]

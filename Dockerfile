# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/sigame ./cmd/sigame

# ffprobe is optional; the alpine image is used so it can be added with one apk line.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata ffmpeg
COPY --from=build /out/sigame /usr/local/bin/sigame
ENV SIGAME_ADDR=:8080 SIGAME_DATA_DIR=/data SIGAME_LOG_FORMAT=json
VOLUME ["/data"]
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/sigame"]

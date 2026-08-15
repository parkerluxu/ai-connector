FROM golang:1.24-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ai-connector ./cmd/ai-connector

FROM alpine:3.22
RUN adduser -D -u 10001 connector \
    && mkdir -p /data \
    && chown connector:connector /data
USER connector
COPY --from=build /out/ai-connector /usr/local/bin/ai-connector
VOLUME ["/data"]
ENTRYPOINT ["ai-connector", "observe", "serve"]

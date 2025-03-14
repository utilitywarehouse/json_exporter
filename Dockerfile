FROM golang:1.23.4-alpine AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY . .

# Build
RUN go test -v -cover ./... \
    && CGO_ENABLED=0 go build -a -o json_exporter

FROM alpine:3.18

ENV USER_ID=65532

# create a system user without home dir
RUN adduser -S -H -u $USER_ID appuser \
      && apk --no-cache add ca-certificates

WORKDIR /
COPY --from=builder /workspace/json_exporter .

ENV USER=appuser
USER $USER_ID

ENTRYPOINT ["/json_exporter"]

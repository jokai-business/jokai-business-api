FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./internal/document ./platform/documentstorage && go vet ./cmd/s3check \
    && CGO_ENABLED=0 go build -trimpath -o /out/s3check ./cmd/s3check
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0
COPY --from=build /out/s3check /s3check
USER 1000:1000
ENTRYPOINT ["/s3check"]

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate && CGO_ENABLED=0 go build -trimpath -o /out/bootstrap ./cmd/bootstrap
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
RUN mkdir -p /var/lib/business-documents && chown app:app /var/lib/business-documents
COPY --from=build /out/ /app/
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]

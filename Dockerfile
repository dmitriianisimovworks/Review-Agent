FROM golang:1.24-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api ./cmd/api

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates wget

COPY --from=builder /out/api /app/api

EXPOSE 8080

CMD ["/app/api"]

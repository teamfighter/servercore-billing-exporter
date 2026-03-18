# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /servercore-billing-exporter

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /servercore-billing-exporter /usr/local/bin/
EXPOSE 9876
ENTRYPOINT ["servercore-billing-exporter"]

FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
# COPY go.sum ./
RUN go mod download

COPY . .
RUN go build -o metering-service .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/metering-service .
CMD ["./metering-service"]

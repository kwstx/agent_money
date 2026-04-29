FROM golang:1.21-alpine

WORKDIR /app

COPY go.mod ./
# COPY go.sum ./
RUN go mod download

COPY . .

RUN go build -o orchestration-service main.go

EXPOSE 8080 9090

CMD ["./orchestration-service"]

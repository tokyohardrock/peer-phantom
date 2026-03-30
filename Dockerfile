FROM golang:1.26-alpine3.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server cmd/main.go

FROM alpine:3.23

WORKDIR /app

RUN adduser -S -D -H bladerunner

COPY --from=builder /app/server /app/server

RUN chmod +x ./server &&\
    chown -R bladerunner /app

USER bladerunner

EXPOSE 4001

CMD ["./server"]

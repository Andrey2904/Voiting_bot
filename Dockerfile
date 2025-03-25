FROM golang:1.24-alpine

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN go build -o poll-bot ./cmd

CMD ["./poll-bot"]

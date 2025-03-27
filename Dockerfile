FROM golang:1.24

WORKDIR /app

# Устанавливаем системные библиотеки OpenSSL
RUN apt-get update && apt-get install -y \
    pkg-config \
    libssl-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod ./
RUN go mod download

COPY . .

# Ставим флаг CGO, чтобы подключить C-библиотеки
ENV CGO_ENABLED=1

RUN go build -o poll-bot ./cmd

CMD ["./poll-bot"]
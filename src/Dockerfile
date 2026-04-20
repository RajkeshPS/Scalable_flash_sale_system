# Stage 1: Build
FROM golang:1.21-alpine AS builder

WORKDIR /app


# Add Go module proxy for reliable downloads
ENV GOPROXY=https://proxy.golang.org,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o flash-sale ./cmd/server

# Stage 2: Minimal runtime image
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/flash-sale .

EXPOSE 8080

CMD ["./flash-sale"]
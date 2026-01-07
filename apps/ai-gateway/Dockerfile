# Etapa 1: Build
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copia solo los archivos de dependencias primero para cachear capas
COPY go.mod ./
# Si en algún momento tiene go.sum, descomentar la próxima línea
# COPY go.sum ./
RUN go mod download

COPY . .

# Compila el binario estático (CGO_ENABLED=0 para que corra en cualquier linux)
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway ./cmd/gateway/main.go

# Etapa 2: Final (Runtime)
FROM alpine:latest

# Instala certificados para estar preparado cuando llame a Groq o APIs externas (HTTPS)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copia el binario desde la etapa anterior
COPY --from=builder /app/gateway .

# Expone el puerto por defecto
EXPOSE 8080

CMD ["./gateway"]

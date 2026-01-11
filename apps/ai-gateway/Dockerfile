# Etapa 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY apps/ai-gateway/go.mod ./
COPY apps/ai-gateway/go.sum ./

RUN go mod download

COPY apps/ai-gateway/ .

# Compila el binario estático (CGO_ENABLED=0 para que corra en cualquier linux)
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway ./cmd/gateway/main.go

# Etapa 2: Final (Runtime)
FROM alpine:latest

# Instala certificados para estar preparado cuando llame a Groq o APIs externas (HTTPS)
RUN apk --no-cache add ca-certificates

# Cambiamos a /app para que el binario encuentre las keys en la ruta relativa "configs/keys.json"
WORKDIR /app

# Copia el binario desde la etapa anterior
COPY --from=builder /app/gateway .

# Expone el puerto por defecto
EXPOSE 8080

CMD ["./gateway"]

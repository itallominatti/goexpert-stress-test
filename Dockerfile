# Etapa 1: compila um binário estático
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/stress-test .

# Etapa 2: imagem mínima só com o binário e os certificados para HTTPS
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/stress-test /stress-test
USER 65534:65534
ENTRYPOINT ["/stress-test"]

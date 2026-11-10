# goexpert-stress-test

CLI em Go para testes de carga em serviços web. Você informa a URL, o número total de requisições e quantas rodam ao mesmo tempo; no fim, o programa imprime um relatório com tempo total, quantidade de requests, quantos voltaram HTTP 200 e a distribuição dos demais códigos.

Sem dependências externas: só a biblioteca padrão do Go.

## Parâmetros

| Flag | Obrigatória | Padrão | Descrição |
|---|---|---|---|
| `--url` | sim | | URL do serviço (`http://` ou `https://`) |
| `--requests` | sim | | Número total de requisições |
| `--concurrency` | não | `1` | Número de chamadas simultâneas |
| `--timeout` | não | `30s` | Tempo máximo de cada requisição (`500ms`, `10s`, `1m`...) |
| `--method` | não | `GET` | Método HTTP |

## Rodando com Docker

### 1. Build da imagem

```bash
docker build -t goexpert-stress-test .
```

A imagem final parte de `scratch` (só o binário estático e os certificados para HTTPS) e tem cerca de 5 MB.

### 2. Executando o teste

```bash
docker run --rm goexpert-stress-test --url=http://google.com --requests=1000 --concurrency=10
```

Para testar um serviço que está rodando na sua própria máquina, use `host.docker.internal` no lugar de `localhost` (no Linux, adicione `--add-host=host.docker.internal:host-gateway`):

```bash
docker run --rm goexpert-stress-test --url=http://host.docker.internal:8080/health --requests=500 --concurrency=50
```

Ajuda:

```bash
docker run --rm goexpert-stress-test --help
```

## Exemplo de relatório

```
Iniciando teste: 1000 requests para http://google.com com concorrência 10...

====================================================
 RELATÓRIO DO TESTE DE CARGA
====================================================
 URL:                     http://google.com
 Concorrência:            10
 Tempo total:             1m38.904s
 Requests solicitados:    1000
 Requests realizados:     1000
 Requests com HTTP 200:   625
 Vazão média:             10.11 req/s

 Distribuição dos códigos HTTP:
   200 OK                                625  ( 62.5%)
   301 Moved Permanently                   1  (  0.1%)
   302 Found                             109  ( 10.9%)
   429 Too Many Requests                 265  ( 26.5%)

 Latência por request:
   mín 443.2ms | média 986.7ms | p50 536.66ms | p90 3.2s | p99 3.937s | máx 4.241s
====================================================
```

O Google limita quem faz muitas requisições seguidas, por isso aparecem `429` e `302` nesse exemplo.

O que cada parte significa:

- **Tempo total**: do primeiro request disparado até o último terminar.
- **Requests realizados**: sempre igual a `--requests` (a menos que o teste seja interrompido com Ctrl+C).
- **Requests com HTTP 200**: respostas com status 200.
- **Distribuição dos códigos HTTP**: contagem de cada status recebido (200, 404, 500...).
- **Falhas sem resposta HTTP**: só aparece quando há requests que não chegaram a receber status (timeout, conexão recusada, DNS, TLS). Eles também entram no total de requests realizados.
- **Latência**: mínimo, média, percentis 50/90/99 e máximo de cada request.

## Como funciona

- As `--requests` requisições são colocadas numa fila (canal) com exatamente esse número de itens.
- `--concurrency` goroutines consomem a fila; cada item vira uma requisição. Assim o total é cumprido exato e nunca há mais chamadas simultâneas do que o pedido. Se `--concurrency` for maior que `--requests`, usa-se só o necessário.
- O cliente HTTP reaproveita conexões (keep-alive) com um pool do tamanho da concorrência e lê o corpo inteiro de cada resposta.
- Redirecionamentos são seguidos (até 10). Num laço de redirecionamento, conta-se o último código recebido (ex.: `302`).
- Ctrl+C interrompe o teste e imprime o relatório do que já foi feito.

## Rodando sem Docker

Com Go 1.23+:

```bash
go run . --url=http://google.com --requests=100 --concurrency=10
```

Testes:

```bash
go test -race ./...
```

Ou, sem Go instalado:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.23 go test -race ./...
```

## Estrutura

```
main.go                       leitura das flags e saída do programa
internal/loadtest/runner.go   execução concorrente das requisições
internal/loadtest/report.go   consolidação e impressão do relatório
Dockerfile                    build multi-stage, imagem final a partir de scratch
```

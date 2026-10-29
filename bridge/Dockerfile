# MCP:RSV Data — HTTP-прокси к HTTP-сервису 1С.
# Целевая платформа: Linux (amd64). STDIO-транспорт не используется.
#
# Сборка из каталога bridge/:
#   docker build -t rsvdata-bridge .
#
# Запуск:
#   docker run --rm -p 8787:8787 \
#     -e RSV_UPSTREAM=http://host.docker.internal/UT/hs/rsvdata/mcp \
#     -e RSV_USER=Администратор \
#     -e RSV_PASSWORD= \
#     rsvdata-bridge

FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# go.mod может требовать более новый toolchain — Go подтянет его сам.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/rsvdata-bridge .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/rsvdata-bridge /rsvdata-bridge

ENV RSV_LISTEN=:8787
ENV RSV_PATH=/mcp

EXPOSE 8787
USER nonroot:nonroot
ENTRYPOINT ["/rsvdata-bridge"]
CMD ["serve"]

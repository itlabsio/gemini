FROM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
RUN sed -i 's|https://dl-cdn.alpinelinux.org|https://mirror.yandex.ru/mirrors|g' /etc/apk/repositories && \
    apk update && \
    apk upgrade && \
    apk add ca-certificates && \
    update-ca-certificates
COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/gemini ./cmd/gemini

# Финальный образ несёт и API-сервер, и dump/restore-раннер, поэтому нужны
# клиентские утилиты PostgreSQL (подкоманды `gemini dump` / `gemini restore`).
# pg_dump -Fc жмёт сам (zlib в бинаре), отдельный gzip не нужен.
#
# Несколько мажоров клиента (postgresql<N>-client, бинари в
# /usr/libexec/postgresql<N>/): архив pg_dump -Fc несёт формат своей мажорной
# версии и список GUC, которые pg_restore выставляет на целевом сервере. Дамп
# снимаем pg_dump'ом мажора источника, restore — pg_restore'ом мажора архива
# (см. backend/internal/jobrunner/pgclient.go). Набор мажоров держать синхронно
# с alpine (сейчас доступны 16/17/18).
FROM alpine:3 AS final
ADD "https://storage.yandexcloud.net/cloud-certs/CA.pem" /usr/local/share/ca-certificates/YandexCA.crt
ADD "https://storage.yandexcloud.net/itlabs-ca/devops/root.crt" /usr/local/share/ca-certificates/itlabs-devops-root.crt
RUN sed -i 's|https://dl-cdn.alpinelinux.org|https://mirror.yandex.ru/mirrors|g' /etc/apk/repositories && \
    apk update && \
    apk upgrade && \
    apk add ca-certificates postgresql16-client postgresql17-client postgresql18-client tzdata && \
    update-ca-certificates && \
    addgroup -g 10001 -S gemini && adduser -u 10001 -S -G gemini gemini
COPY --from=build /out/gemini /usr/local/bin/gemini
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["gemini"]
CMD ["serve"]

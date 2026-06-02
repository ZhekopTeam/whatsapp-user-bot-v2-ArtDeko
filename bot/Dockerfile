# Сборка Telegram-бота администрирования вместе с Go-бинарником WhatsApp-бота,
# который выполняет QR-авторизацию (общий формат сессий whatsmeow в sessions/multi.db).

# --- Go: бинарник авторизации/прогрева ---
FROM golang:1.24-bookworm AS go-builder

WORKDIR /src
ENV GOTOOLCHAIN=auto

RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /out/wh-user-bot ./

# --- Python: зависимости бота ---
FROM python:3.12-slim AS py-builder

WORKDIR /build

RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential \
 && rm -rf /var/lib/apt/lists/*

COPY bot/requirements.txt .
RUN python -m venv /opt/venv \
 && /opt/venv/bin/python -m pip install --upgrade pip \
 && /opt/venv/bin/pip install --no-cache-dir -r requirements.txt

# --- Runtime ---
FROM python:3.12-slim AS production

WORKDIR /bot

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY --from=py-builder /opt/venv /opt/venv
COPY --from=go-builder /out/wh-user-bot /app/wh-user-bot

ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/bot \
    WHATSAPP_BIN=/app/wh-user-bot

COPY bot/ /bot

RUN mkdir -p /bot/sessions /bot/data

ENTRYPOINT ["python"]
CMD ["main.py"]

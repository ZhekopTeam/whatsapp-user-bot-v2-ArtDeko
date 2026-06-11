# --- Python: dependencies builder ---
FROM python:3.12-slim AS py-builder

WORKDIR /build

RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential \
 && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN python -m venv /opt/venv \
 && /opt/venv/bin/python -m pip install --upgrade pip \
 && /opt/venv/bin/pip install --no-cache-dir -r requirements.txt

# --- Runtime ---
FROM python:3.12-slim AS production

WORKDIR /admin-tg-bot

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY --from=py-builder /opt/venv /opt/venv

ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/admin-tg-bot

COPY . /admin-tg-bot

RUN mkdir -p /admin-tg-bot/sessions /admin-tg-bot/data

ENTRYPOINT ["python"]
CMD ["main.py"]
